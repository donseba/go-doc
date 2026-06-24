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
        val source = index.source ?: "no .go-doc/index.json or .partial/index.json found"
        val relative = relativePath(project.basePath, filePath)
        val contract = index.contractForFile(project, filePath)
        val content = listOf(
            "Source: $source",
            "Index root: ${index.rootPath ?: "-"}",
            "Project: ${project.basePath ?: "-"}",
            "File: ${relative ?: "-"}",
            "Contract: ${if (contract == null) "not matched" else "matched"}",
            "Templates: ${index.templates.size}",
            "Types: ${index.types.size}",
            "Error: ${index.loadError ?: "-"}",
            "Checked:",
            index.checkedPaths.take(12).joinToString("\n") { "  $it" },
        ).joinToString("\n")

        Messages.showInfoMessage(project, content, "go-doc Index Status")
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
