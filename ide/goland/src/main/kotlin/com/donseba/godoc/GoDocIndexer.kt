package com.donseba.godoc

import java.io.File
import java.util.concurrent.TimeUnit

object GoDocIndexer {
    fun findModuleRoot(filePath: String?): File? {
        if (filePath == null) return null
        var dir = File(filePath).let { if (it.isDirectory) it else it.parentFile }
        while (dir != null) {
            if (File(dir, "go.mod").isFile) return dir
            dir = dir.parentFile
        }
        return null
    }

    fun run(root: File, outFile: File): ProcessResult {
        val commands = listOf(
            listOf("go-doc", "index", "-o", outFile.path, "."),
            listOf("go-doc.exe", "index", "-o", outFile.path, "."),
        )

        var lastError = ""
        var executableMissing = false
        for (command in commands) {
            try {
                val process = ProcessBuilder(command)
                    .directory(root)
                    .redirectErrorStream(false)
                    .start()
                val finished = process.waitFor(60, TimeUnit.SECONDS)
                if (!finished) {
                    process.destroyForcibly()
                    return ProcessResult(1, "", "go-doc index timed out after 60 seconds")
                }
                val stdout = process.inputStream.bufferedReader().readText()
                val stderr = process.errorStream.bufferedReader().readText()
                return ProcessResult(process.exitValue(), stdout, stderr)
            } catch (err: Exception) {
                executableMissing = true
                lastError = err.message ?: err.javaClass.simpleName
            }
        }

        return ProcessResult(
            1,
            "",
            "Could not run go-doc from PATH. Install it with: go install github.com/donseba/go-doc@latest\n$lastError",
            missingGoDoc = executableMissing,
        )
    }

    fun install(root: File): ProcessResult {
        val commands = listOf(
            listOf("go", "install", "github.com/donseba/go-doc@latest"),
            listOf("go.exe", "install", "github.com/donseba/go-doc@latest"),
        )

        var lastError = ""
        var executableMissing = false
        for (command in commands) {
            try {
                val process = ProcessBuilder(command)
                    .directory(root)
                    .redirectErrorStream(false)
                    .start()
                val finished = process.waitFor(120, TimeUnit.SECONDS)
                if (!finished) {
                    process.destroyForcibly()
                    return ProcessResult(1, "", "go install timed out after 120 seconds")
                }
                val stdout = process.inputStream.bufferedReader().readText()
                val stderr = process.errorStream.bufferedReader().readText()
                return ProcessResult(process.exitValue(), stdout, stderr)
            } catch (err: Exception) {
                executableMissing = true
                lastError = err.message ?: err.javaClass.simpleName
            }
        }

        return ProcessResult(
            1,
            "",
            "Could not run Go from PATH. Install Go or add it to PATH before installing go-doc.\n$lastError",
            missingGo = executableMissing,
        )
    }

    data class ProcessResult(
        val exitCode: Int,
        val stdout: String,
        val stderr: String,
        val missingGoDoc: Boolean = false,
        val missingGo: Boolean = false,
    )
}
