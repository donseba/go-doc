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
        val file = sourceElement?.containingFile ?: return null
        val virtualFile = file.virtualFile ?: return null
        if (!isSupportedTemplate(virtualFile)) return null

        val project = file.project
        val index = GoDocIndex.load(project, virtualFile.path)
        val contract = index.contractForFile(project, virtualFile.path) ?: return null
        GoDocTemplateContext.templateFunctionAt(file.text, offset, index, contract)?.let { reference ->
            val fn = index.funcs[reference.funcName] ?: return null
            return targetElement(project, index, fn.file, fn.line, fn.column)
        }

        val reference = GoDocTemplateContext.fieldReferenceAt(file.text, offset, index, contract) ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
        val field = owner.fields[reference.memberName]
        if (field != null) {
            return targetElement(project, index, field.file.ifBlank { owner.file }, field.line, field.column)
        }
        val method = owner.methods[reference.memberName] ?: return null
        return targetElement(project, index, method.file.ifBlank { owner.file }, method.line, method.column)
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
        val targetVirtualFile = LocalFileSystem.getInstance()
            .findFileByIoFile(File(root, targetFile)) ?: return null
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
}
