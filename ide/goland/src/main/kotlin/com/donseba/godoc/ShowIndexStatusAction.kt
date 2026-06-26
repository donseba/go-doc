package com.donseba.godoc

import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.ui.Messages
import java.io.File

class ShowIndexStatusAction : AnAction() {
    override fun actionPerformed(event: AnActionEvent) {
        val project = event.project ?: return
        val filePath = event.getData(com.intellij.openapi.actionSystem.CommonDataKeys.VIRTUAL_FILE)?.path
        val index = GoDocIndex.load(project, filePath)
        val source = index.source ?: "no optional .go-doc/index.json file"
        val relative = relativePath(project.basePath, filePath)
        val contract = index.contractForFile(project, filePath)
        val root = GoDocIndexer.findModuleRoot(filePath ?: project.basePath) ?: project.basePath?.let { File(it) }
        val installedVersion = root?.let { GoDocIndexer.commandVersion("go-doc", it) } ?: "-"
        val shadowIndex = root?.let { GoDocIndexer.shadowIndexFile(it) }
        val content = listOf(
            "Index source: $source",
            "Shadow index: ${shadowIndex?.path ?: "-"}",
            "Shadow exists: ${shadowIndex?.isFile ?: false}",
            "Enabled: ${root?.let { GoDocIndexer.enabled(project, it) } ?: "-"}",
            "Write project index: ${root?.let { GoDocIndexer.autoIndexEnabled(project, it) } ?: "-"}",
            "Index root: ${index.rootPath ?: "-"}",
            "Project: ${project.basePath ?: "-"}",
            "File: ${relative ?: "-"}",
            "Installed version: $installedVersion",
            "LSP executable: ${GoDocIndexer.lastLspExecutable ?: "-"}",
            "LSP version: ${GoDocIndexer.lastLspVersion ?: "-"}",
            "Contract: ${if (contract == null) "not matched" else "matched"}",
            "Templates: ${index.templates.size}",
            "Types: ${index.types.size}",
            "Error: ${index.loadError ?: "-"}",
            "Checked:",
            index.checkedPaths.take(12).joinToString("\n") { "  $it" },
        ).joinToString("\n")

        Messages.showInfoMessage(project, content, "go-doc Status")
    }

    private fun relativePath(basePath: String?, filePath: String?): String? {
        if (basePath == null || filePath == null) return filePath
        return try {
            File(basePath).toPath().relativize(File(filePath).toPath()).toString()
        } catch (_: Exception) {
            filePath
        }
    }
}
