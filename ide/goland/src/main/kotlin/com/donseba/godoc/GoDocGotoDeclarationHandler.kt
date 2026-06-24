package com.donseba.godoc

import com.intellij.codeInsight.navigation.actions.GotoDeclarationHandler
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.fileEditor.FileDocumentManager
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
        if (virtualFile.extension !in setOf("gohtml", "tmpl", "html")) return null

        val project = file.project
        val index = GoDocIndex.load(project, virtualFile.path)
        val contract = index.contractForFile(project, virtualFile.path) ?: return null
        TemplateContext.typeReferenceAt(file.text, offset, index)?.let { reference ->
            val type = index.types[reference.typeName] ?: return null
            return targetElement(project, index, type.file, type.line, type.column)
        }

        val reference = TemplateContext.fieldReferenceAt(file.text, offset, index, contract) ?: return null
        val field = index.types[reference.ownerTypeName]?.fields?.get(reference.fieldName) ?: return null
        val targetFile = field.file.ifBlank { index.types[reference.ownerTypeName]?.file.orEmpty() }
        return targetElement(project, index, targetFile, field.line, field.column)
    }

    private fun targetElement(
        project: com.intellij.openapi.project.Project,
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
