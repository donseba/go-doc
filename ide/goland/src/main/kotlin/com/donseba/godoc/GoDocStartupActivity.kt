package com.donseba.godoc

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.Task
import com.intellij.openapi.project.Project
import com.intellij.openapi.startup.ProjectActivity
import java.io.File

class GoDocStartupActivity : ProjectActivity {
    override suspend fun execute(project: Project) {
        GoDocIndexWatcher.install(project)

        val basePath = goDocReadAction { project.basePath } ?: return
        val root = GoDocIndexer.findModuleRoot(basePath) ?: File(basePath)
        if (!File(root, "go.mod").isFile) return
        if (!GoDocIndexer.enabled(project, root)) return

        object : Task.Backgroundable(project, "Building go-doc index", false) {
            override fun run(indicator: ProgressIndicator) {
                indicator.text = "Running go-doc index"
                val outFile = GoDocIndexer.indexTarget(project, root)
                if (GoDocIndexer.autoIndexEnabled(project, root) && outFile.isFile) return
                outFile.parentFile.mkdirs()
                val result = GoDocIndexer.run(root, outFile)
                if (result.exitCode != 0) {
                    if (result.missingGoDoc) {
                        GoDocCliInstaller.offerInstallAndIndex(project, root, outFile, indexMessage(root, outFile))
                        return
                    }
                    notify(project, "go-doc index not built", result.stderr.ifBlank { result.stdout }, NotificationType.WARNING)
                    return
                }
                ApplicationManager.getApplication().invokeLater {
                    GoDocIndex.refreshVirtualIndex(project)
                    GoDocEditorRefresh.refresh(project)
                    if (outFile.isFile) {
                        notify(project, "go-doc index built", indexMessage(root, outFile), NotificationType.INFORMATION)
                    }
                }
            }
        }.queue()
    }

    private fun indexMessage(root: File, outFile: File): String {
        return if (outFile.path.startsWith(root.path)) ".go-doc/index.json created" else "go-doc shadow index created"
    }

    private fun notify(project: Project, title: String, content: String, type: NotificationType) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup("go-doc")
            .createNotification(title, content.take(800), type)
            .notify(project)
    }
}
