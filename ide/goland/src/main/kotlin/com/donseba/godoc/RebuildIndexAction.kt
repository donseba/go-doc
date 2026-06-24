package com.donseba.godoc

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.AnAction
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.CommonDataKeys
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.Task
import com.intellij.openapi.project.Project
import java.io.File

class RebuildIndexAction : AnAction() {
    override fun actionPerformed(event: AnActionEvent) {
        val project = event.project ?: return
        val selectedPath = event.getData(CommonDataKeys.VIRTUAL_FILE)?.path
        object : Task.Backgroundable(project, "Rebuilding go-doc index", false) {
            override fun run(indicator: ProgressIndicator) {
                indicator.text = "Running go-doc index"
                rebuild(project, selectedPath)
            }
        }.queue()
    }

    private fun rebuild(project: Project, selectedPath: String?) {
        val root = GoDocIndexer.findModuleRoot(selectedPath) ?: project.basePath?.let { File(it) } ?: return
        val outDir = File(root, ".go-doc")
        val outFile = File(outDir, "index.json")
        outDir.mkdirs()

        val result = GoDocIndexer.run(root, outFile)
        if (result.exitCode != 0) {
            if (result.missingGoDoc) {
                GoDocCliInstaller.offerInstallAndIndex(project, root, outFile, ".go-doc/index.json updated")
                return
            }
            notify(project, "go-doc index failed", result.stderr.ifBlank { result.stdout }, NotificationType.ERROR)
            return
        }

        ApplicationManager.getApplication().invokeLater {
            GoDocIndex.refreshVirtualIndex(project)
            if (outFile.isFile) {
                notify(project, "go-doc index rebuilt", ".go-doc/index.json updated", NotificationType.INFORMATION)
            } else {
                notify(project, "go-doc index not needed", "No @model annotations found; index not written.", NotificationType.INFORMATION)
            }
        }
    }

    private fun notify(project: Project, title: String, content: String, type: NotificationType) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup("go-doc")
            .createNotification(title, content.take(800), type)
            .notify(project)
    }
}
