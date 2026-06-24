package com.donseba.godoc

import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.actionSystem.ToggleAction

class ToggleAutoIndexAction : ToggleAction() {
    override fun isSelected(event: AnActionEvent): Boolean {
        val project = event.project ?: return false
        return GoDocSettings.getInstance(project).state.autoIndex
    }

    override fun setSelected(event: AnActionEvent, state: Boolean) {
        val project = event.project ?: return
        GoDocSettings.getInstance(project).state.autoIndex = state
        val status = if (state) "enabled" else "disabled"
        NotificationGroupManager.getInstance()
            .getNotificationGroup("go-doc")
            .createNotification("go-doc auto-index $status", "Automatic index rebuilding is now $status for this project.", NotificationType.INFORMATION)
            .notify(project)
    }
}
