package com.donseba.godoc

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.util.Computable

internal fun <T> goDocReadAction(action: () -> T): T {
    val application = ApplicationManager.getApplication()
    return if (application.isReadAccessAllowed) {
        action()
    } else {
        application.runReadAction(Computable { action() })
    }
}
