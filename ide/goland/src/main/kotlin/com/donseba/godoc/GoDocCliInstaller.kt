package com.donseba.godoc

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.Task
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import java.io.File

object GoDocCliInstaller {
    fun offerInstallAndIndex(project: Project, root: File, outFile: File, successMessage: String) {
        ApplicationManager.getApplication().invokeLater {
            val answer = Messages.showYesNoDialog(
                project,
                "go-doc is not available on PATH.\n\nInstall it now with:\n\ngo install github.com/donseba/go-doc@latest",
                "Install go-doc CLI",
                "Install",
                "Cancel",
                Messages.getQuestionIcon(),
            )
            if (answer != Messages.YES) return@invokeLater

            object : Task.Backgroundable(project, "Installing go-doc CLI", false) {
                override fun run(indicator: ProgressIndicator) {
                    indicator.text = "Running go install github.com/donseba/go-doc@latest"
                    val install = GoDocIndexer.install(root)
                    if (install.exitCode != 0) {
                        notify(project, "go-doc install failed", install.stderr.ifBlank { install.stdout }, NotificationType.ERROR)
                        return
                    }

                    indicator.text = "Running go-doc index"
                    val result = GoDocIndexer.run(root, outFile)
                    if (result.exitCode != 0) {
                        notify(project, "go-doc index failed", result.stderr.ifBlank { result.stdout }, NotificationType.ERROR)
                        return
                    }

                    ApplicationManager.getApplication().invokeLater {
                        GoDocIndex.refreshVirtualIndex(project)
                        GoDocEditorRefresh.refresh(project)
                        if (outFile.isFile) {
                            notify(project, "go-doc index rebuilt", successMessage, NotificationType.INFORMATION)
                        } else {
                            notify(project, "go-doc index not needed", "No template contracts found; index not written.", NotificationType.INFORMATION)
                        }
                    }
                }
            }.queue()
        }
    }

    private fun notify(project: Project, title: String, content: String, type: NotificationType) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup("go-doc")
            .createNotification(title, content.take(800), type)
            .notify(project)
    }
}
