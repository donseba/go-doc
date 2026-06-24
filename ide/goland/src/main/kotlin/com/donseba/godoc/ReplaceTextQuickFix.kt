package com.donseba.godoc

import com.intellij.codeInsight.intention.IntentionAction
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.TextRange
import com.intellij.psi.PsiFile

class ReplaceTextQuickFix(
    private val range: TextRange,
    private val replacement: String,
    private val label: String,
) : IntentionAction {
    override fun getText(): String = label

    override fun getFamilyName(): String = "go-doc"

    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?): Boolean {
        val document = editor?.document ?: return false
        return range.startOffset >= 0 && range.endOffset <= document.textLength
    }

    override fun invoke(project: Project, editor: Editor?, file: PsiFile?) {
        val document = editor?.document ?: return
        document.replaceString(range.startOffset, range.endOffset, replacement)
    }

    override fun startInWriteAction(): Boolean = true
}
