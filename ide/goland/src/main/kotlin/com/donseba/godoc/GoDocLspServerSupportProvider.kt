@file:Suppress("DEPRECATION")

package com.donseba.godoc

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.editor.markup.TextAttributes
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
import com.intellij.platform.lsp.api.customization.LspCustomization
import com.intellij.platform.lsp.api.customization.LspSemanticTokensSupport
import com.intellij.psi.PsiFile
import com.intellij.ui.JBColor
import java.awt.Font
import java.io.File
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.concurrent.TimeUnit

internal class GoDocLspServerSupportProvider : LspServerSupportProvider {
    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter,
    ) {
        val root = GoDocIndexer.findModuleRoot(file.path) ?: goDocReadAction { project.basePath }?.let { java.io.File(it) }
        if (isSupportedTemplate(file) && (root == null || GoDocIndexer.enabled(project, root))) {
            serverStarter.ensureServerStarted(GoDocLspServerDescriptor(project))
        }
    }
}

private class GoDocLspServerDescriptor(project: Project) : ProjectWideLspServerDescriptor(project, "go-doc") {
    override val lspCustomization = object : LspCustomization() {
        override val semanticTokensCustomizer = object : LspSemanticTokensSupport() {
            override val tokenTypes: List<String> = listOf("variable", "property", "type", "function")
            override val tokenModifiers: List<String> = emptyList()

            override fun shouldAskServerForSemanticTokens(psiFile: PsiFile): Boolean {
                return goDocReadAction { isSupportedTemplate(psiFile.virtualFile) }
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
    }

    override fun isSupportedFile(file: VirtualFile): Boolean {
        val root = GoDocIndexer.findModuleRoot(file.path) ?: goDocReadAction { project.basePath }?.let { java.io.File(it) }
        return isSupportedTemplate(file) && (root == null || GoDocIndexer.enabled(project, root))
    }

    override fun createCommandLine(): GeneralCommandLine {
        val root = goDocReadAction { project.basePath } ?: "."
        val executable = goDocLspExecutable(root)
        GoDocIndexer.rememberLspExecutable(executable, File(root))
        return GeneralCommandLine(executable, "lsp", root).withWorkDirectory(root)
    }
}

internal fun isSupportedTemplate(file: VirtualFile): Boolean {
    return file.extension in setOf("gohtml", "tmpl", "html")
}

private val GO_DOC_ACCESSOR = clickableKey("GO_DOC_ACCESSOR", JBColor(0xC586C0, 0xC586C0))
private val GO_DOC_FIELD = clickableKey("GO_DOC_FIELD", JBColor(0x9CDCFE, 0x9CDCFE))
private val GO_DOC_TYPE = clickableKey("GO_DOC_TYPE", JBColor(0x4EC9B0, 0x4EC9B0))
private val GO_DOC_FUNCTION = clickableKey("GO_DOC_FUNCTION", JBColor(0xDCDCAA, 0xDCDCAA))

@Suppress("DEPRECATION")
private fun clickableKey(name: String, color: JBColor): TextAttributesKey {
    return TextAttributesKey.createTextAttributesKey(
        name,
        TextAttributes(color, null, null, null, Font.PLAIN),
    )
}

private fun goDocLspExecutable(root: String): String {
    if (!isWindows()) return "go-doc"
    val installed = findGoDocExecutable(root) ?: return "go-doc"

    return try {
        val cacheDir = File(System.getProperty("java.io.tmpdir"), "go-doc-goland-lsp")
        cacheDir.mkdirs()
        cleanupOldLspCopies(cacheDir)
        val copy = File(cacheDir, "go-doc-lsp-${ProcessHandle.current().pid()}-${System.currentTimeMillis()}.exe")
        Files.copy(installed.toPath(), copy.toPath(), StandardCopyOption.REPLACE_EXISTING)
        copy.absolutePath
    } catch (_: Exception) {
        installed.absolutePath
    }
}

private fun findGoDocExecutable(root: String): File? {
    val locators = if (isWindows()) listOf("where.exe", "where") else listOf("which")
    for (locator in locators) {
        try {
            val process = ProcessBuilder(locator, "go-doc")
                .directory(File(root))
                .redirectErrorStream(true)
                .start()
            if (!process.waitFor(5, TimeUnit.SECONDS)) {
                process.destroyForcibly()
                continue
            }
            process.inputStream.bufferedReader().readLines()
                .map { File(it.trim()) }
                .firstOrNull { it.isFile }
                ?.let { return it }
        } catch (_: Exception) {
            continue
        }
    }
    return null
}

private fun cleanupOldLspCopies(cacheDir: File) {
    cacheDir.listFiles { file -> file.name.matches(Regex("""go-doc-lsp-\d+-\d+\.exe""")) }
        ?.forEach { file ->
            runCatching { file.delete() }
        }
}

private fun isWindows(): Boolean {
    return System.getProperty("os.name").lowercase().contains("win")
}
