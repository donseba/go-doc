package com.donseba.godoc

import com.intellij.codeInsight.daemon.DaemonCodeAnalyzer
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project

object GoDocEditorRefresh {
    fun refresh(project: Project) {
        ApplicationManager.getApplication().invokeLater {
            if (!project.isDisposed) {
                DaemonCodeAnalyzer.getInstance(project).restart("go-doc index refreshed")
            }
        }
    }
}
