package com.donseba.godoc

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.Key
import com.intellij.openapi.vfs.VirtualFileManager
import com.intellij.openapi.vfs.newvfs.BulkFileListener
import com.intellij.openapi.vfs.newvfs.events.VFileEvent
import com.intellij.util.Alarm
import java.io.File

object GoDocIndexWatcher {
    private val installedKey = Key.create<Boolean>("go-doc.index-watcher.installed")
    private const val debounceMillis = 1200

    fun install(project: Project) {
        if (project.getUserData(installedKey) == true) return
        project.putUserData(installedKey, true)

        val alarm = Alarm(Alarm.ThreadToUse.POOLED_THREAD, project)
        val connection = project.messageBus.connect(project)
        connection.subscribe(
            VirtualFileManager.VFS_CHANGES,
            object : BulkFileListener {
                override fun after(events: MutableList<out VFileEvent>) {
                    val changedPath = events
                        .mapNotNull { event -> event.file?.path ?: event.path }
                        .firstOrNull { shouldTriggerIndex(project, it) }
                        ?: return

                    val root = GoDocIndexer.findModuleRoot(changedPath) ?: return
                    schedule(project, root, alarm)
                }
            },
        )
    }

    private fun schedule(project: Project, root: File, alarm: Alarm) {
        if (!GoDocIndexer.enabled(project, root)) return

        alarm.cancelAllRequests()
        alarm.addRequest(
            {
                val outFile = GoDocIndexer.indexTarget(project, root)
                outFile.parentFile.mkdirs()

                val result = GoDocIndexer.run(root, outFile)
                if (result.exitCode != 0) {
                    notify(project, "go-doc auto-index failed", result.stderr.ifBlank { result.stdout }, NotificationType.WARNING)
                    return@addRequest
                }

                ApplicationManager.getApplication().invokeLater {
                    GoDocIndex.refreshVirtualIndex(project)
                }
            },
            debounceMillis,
        )
    }

    private fun shouldTriggerIndex(project: Project, path: String): Boolean {
        val basePath = project.basePath ?: return false
        val normalized = path.replace('\\', '/')
        if (!normalized.startsWith(basePath.replace('\\', '/'))) return false
        if (ignoredPath(normalized)) return false
        return normalized.endsWith(".go") ||
            normalized.endsWith(".gohtml") ||
            normalized.endsWith(".tmpl") ||
            normalized.endsWith(".html")
    }

    private fun ignoredPath(path: String): Boolean {
        val parts = path.split('/')
        return parts.any {
            it == ".git" ||
                it == ".idea" ||
                it == ".go-doc" ||
                it == "build" ||
                it == "out" ||
                it == "vendor" ||
                it == "node_modules"
        }
    }

    private fun notify(project: Project, title: String, content: String, type: NotificationType) {
        NotificationGroupManager.getInstance()
            .getNotificationGroup("go-doc")
            .createNotification(title, content.take(800), type)
            .notify(project)
    }
}
