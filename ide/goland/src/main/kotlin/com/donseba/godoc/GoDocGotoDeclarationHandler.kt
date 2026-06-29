package com.donseba.godoc

import com.intellij.codeInsight.navigation.actions.GotoDeclarationHandler
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiManager
import java.io.File

class GoDocGotoDeclarationHandler : GotoDeclarationHandler {
    override fun getGotoDeclarationTargets(
        sourceElement: PsiElement?,
        offset: Int,
        editor: Editor?,
    ): Array<PsiElement>? {
        return goDocReadAction {
            val file = sourceElement?.containingFile ?: return@goDocReadAction null
            val virtualFile = file.virtualFile ?: return@goDocReadAction null
            if (!isSupportedTemplate(virtualFile)) return@goDocReadAction null

            val project = file.project
            val index = GoDocIndex.load(project, virtualFile.path)
            GoDocTemplateContext.templateIncludeAt(file.text, offset, index)?.let { reference ->
                return@goDocReadAction targetElement(project, index, reference.targetPath, reference.targetLine, reference.targetColumn)
            }

            GoDocTemplateContext.typeReferenceAt(file.text, offset, index)?.let { reference ->
                val type = index.types[reference.typeName] ?: return@goDocReadAction null
                return@goDocReadAction targetElement(project, index, type.file, type.line, type.column)
            }

            GoDocTemplateContext.typedRootReferenceAt(file.text, offset, index)?.let { reference ->
                val type = index.types[reference.typeName] ?: return@goDocReadAction null
                return@goDocReadAction targetElement(project, index, type.file, type.line, type.column)
            }

            val contract = index.contractForFileAt(project, virtualFile.path, offset) ?: return@goDocReadAction null
            GoDocTemplateContext.templateFunctionAt(file.text, offset, index, contract)?.let { reference ->
                val fn = index.funcs[reference.funcName] ?: return@goDocReadAction null
                return@goDocReadAction targetElement(project, index, fn.file, fn.line, fn.column)
            }

            val reference = GoDocTemplateContext.fieldReferenceAt(file.text, offset, index, contract) ?: return@goDocReadAction null
            val owner = index.types[reference.ownerTypeName] ?: return@goDocReadAction null
            if (contract.isTypedRoot(reference.memberName, reference.ownerTypeName)) {
                return@goDocReadAction targetElement(project, index, owner.file, owner.line, owner.column)
            }
            val field = owner.fields[reference.memberName]
            if (field != null) {
                return@goDocReadAction targetElement(project, index, field.file.ifBlank { owner.file }, field.line, field.column)
            }
            val method = owner.methods[reference.memberName] ?: return@goDocReadAction null
            targetElement(project, index, method.file.ifBlank { owner.file }, method.line, method.column)
        }
    }

    private fun targetElement(
        project: Project,
        index: GoDocIndex,
        targetFile: String,
        targetLine: Int,
        targetColumn: Int,
    ): Array<PsiElement>? {
        if (targetFile.isBlank()) return null
        val root = index.rootPath ?: return null
        val target = targetPath(root, targetFile)
        val targetVirtualFile = LocalFileSystem.getInstance()
            .findFileByIoFile(target) ?: return null
        val targetPsiFile = PsiManager.getInstance(project).findFile(targetVirtualFile) ?: return null
        val document = FileDocumentManager.getInstance().getDocument(targetVirtualFile) ?: return arrayOf(targetPsiFile)
        val line = (targetLine - 1).coerceAtLeast(0)
        val column = (targetColumn - 1).coerceAtLeast(0)
        val targetOffset = if (line < document.lineCount) {
            (document.getLineStartOffset(line) + column).coerceAtMost(document.getLineEndOffset(line))
        } else {
            0
        }

        return arrayOf(targetPsiFile.findElementAt(targetOffset) ?: targetPsiFile)
    }

    private fun targetPath(root: String, targetFile: String): File {
        if (targetFile.startsWith("\$GOROOT")) {
            val goRoot = GoDocIndexer.goRoot(File(root)) ?: return File(root, targetFile)
            return File(goRoot, targetFile.removePrefix("\$GOROOT").trimStart('/', '\\'))
        }
        val file = File(targetFile)
        return if (file.isAbsolute) file else File(root, targetFile)
    }
}
