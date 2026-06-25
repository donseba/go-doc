@file:Suppress("DEPRECATION")

package com.donseba.godoc

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.editor.markup.TextAttributes
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
import com.intellij.platform.lsp.api.customization.LspSemanticTokensSupport
import com.intellij.psi.PsiFile
import com.intellij.ui.JBColor
import java.awt.Font

internal class GoDocLspServerSupportProvider : LspServerSupportProvider {
    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter,
    ) {
        if (isSupportedTemplate(file)) {
            serverStarter.ensureServerStarted(GoDocLspServerDescriptor(project))
        }
    }
}

private class GoDocLspServerDescriptor(project: Project) : ProjectWideLspServerDescriptor(project, "go-doc") {
    @Deprecated("JetBrains keeps semantic token support on this compatibility property in 2025.x.")
    override val lspSemanticTokensSupport = object : LspSemanticTokensSupport() {
        override val tokenTypes: List<String> = listOf("variable", "property", "type", "function")
        override val tokenModifiers: List<String> = emptyList()

        override fun shouldAskServerForSemanticTokens(psiFile: PsiFile): Boolean {
            return isSupportedTemplate(psiFile.virtualFile)
        }

        override fun getTextAttributesKey(
            tokenType: String,
            modifiers: List<String>,
        ): TextAttributesKey? {
            return when (tokenType) {
                "variable" -> GO_DOC_ACCESSOR
                "property" -> GO_DOC_FIELD
                "type" -> GO_DOC_TYPE
                "function" -> GO_DOC_FUNCTION
                else -> null
            }
        }
    }

    override fun isSupportedFile(file: VirtualFile): Boolean {
        return isSupportedTemplate(file)
    }

    override fun createCommandLine(): GeneralCommandLine {
        val root = project.basePath ?: "."
        return GeneralCommandLine("go-doc", "lsp", root).withWorkDirectory(root)
    }
}

internal fun isSupportedTemplate(file: VirtualFile): Boolean {
    return file.extension in setOf("gohtml", "tmpl", "html")
}

private val GO_DOC_ACCESSOR = clickableKey("GO_DOC_ACCESSOR", JBColor(0xC586C0, 0xC586C0))
private val GO_DOC_FIELD = clickableKey("GO_DOC_FIELD", JBColor(0x9CDCFE, 0x9CDCFE))
private val GO_DOC_TYPE = clickableKey("GO_DOC_TYPE", JBColor(0x4EC9B0, 0x4EC9B0))
private val GO_DOC_FUNCTION = clickableKey("GO_DOC_FUNCTION", JBColor(0xDCDCAA, 0xDCDCAA))

private fun clickableKey(name: String, color: JBColor): TextAttributesKey {
    return TextAttributesKey.createTextAttributesKey(
        name,
        TextAttributes(color, null, null, null, Font.PLAIN),
    )
}
