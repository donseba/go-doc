package com.donseba.godoc

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.LocalFileSystem
import java.io.File
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.regex.Pattern
import java.util.concurrent.TimeUnit

object GoDocIndexer {
    private val pendingShadowBuilds = ConcurrentHashMap.newKeySet<String>()

    @Volatile
    private var cachedGoRoot: String? = null

    @Volatile
    var lastLspExecutable: String? = null
        private set

    @Volatile
    var lastLspVersion: String? = null
        private set

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
        return runCommands(root, commands, 60, "go-doc index timed out after 60 seconds")
    }

    fun runStdout(root: File): ProcessResult {
        val commands = listOf(
            listOf("go-doc", "index"),
            listOf("go-doc.exe", "index"),
        )
        return runCommands(root, commands, 60, "go-doc index timed out after 60 seconds")
    }

    private fun runCommands(
        root: File,
        commands: List<List<String>>,
        timeoutSeconds: Long,
        timeoutMessage: String,
    ): ProcessResult {
        var lastError = ""
        var executableMissing = false
        for (command in commands) {
            try {
                val process = ProcessBuilder(command)
                    .directory(root)
                    .redirectErrorStream(false)
                    .start()
                val finished = process.waitFor(timeoutSeconds, TimeUnit.SECONDS)
                if (!finished) {
                    process.destroyForcibly()
                    return ProcessResult(1, "", timeoutMessage)
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

    fun enabled(project: Project, root: File): Boolean {
        if (!GoDocSettings.getInstance(project).state.enabled) return false
        return projectConfigEnabled(root)
    }

    fun autoIndexEnabled(project: Project, root: File): Boolean {
        if (!enabled(project, root)) return false
        projectConfigIndexValue(root)?.let { return it }
        return GoDocSettings.getInstance(project).state.autoIndex
    }

    fun indexTarget(project: Project, root: File): File {
        return if (autoIndexEnabled(project, root)) {
            File(root, ".go-doc/index.json")
        } else {
            shadowIndexFile(root)
        }
    }

    fun shadowIndexFile(root: File): File {
        val digest = MessageDigest.getInstance("SHA-1")
            .digest(root.canonicalPath.toByteArray(Charsets.UTF_8))
            .joinToString("") { "%02x".format(it) }
        val base = File(System.getProperty("java.io.tmpdir"), "go-doc-goland-index")
        return File(File(base, digest), "index.json")
    }

    fun requestShadowIndex(project: Project, root: File) {
        if (!enabled(project, root) || autoIndexEnabled(project, root)) return
        val outFile = shadowIndexFile(root)
        val key = outFile.canonicalPath
        if (!pendingShadowBuilds.add(key)) return

        ApplicationManager.getApplication().executeOnPooledThread {
            try {
                outFile.parentFile.mkdirs()
                val result = run(root, outFile)
                if (result.exitCode == 0) {
                    ApplicationManager.getApplication().invokeLater {
                        LocalFileSystem.getInstance().refreshAndFindFileByIoFile(outFile)
                    }
                }
            } finally {
                pendingShadowBuilds.remove(key)
            }
        }
    }

    private fun projectConfigEnabled(root: File): Boolean {
        val config = File(root, ".go-doc/config.json")
        if (!config.isFile) return true
        val text = runCatching { config.readText() }.getOrNull() ?: return true
        return !Pattern.compile("\"enabled\"\\s*:\\s*false").matcher(text).find()
    }

    private fun projectConfigIndexValue(root: File): Boolean? {
        val config = File(root, ".go-doc/config.json")
        if (!config.isFile) return null
        val text = runCatching { config.readText() }.getOrNull() ?: return null
        if (Pattern.compile("\"writeIndex\"\\s*:\\s*true").matcher(text).find()) return true
        if (Pattern.compile("\"writeIndex\"\\s*:\\s*false").matcher(text).find()) return false
        return null
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

    fun commandVersion(command: String, root: File): String {
        return try {
            val process = ProcessBuilder(command, "version")
                .directory(root)
                .redirectErrorStream(true)
                .start()
            if (!process.waitFor(5, TimeUnit.SECONDS)) {
                process.destroyForcibly()
                return "-"
            }
            process.inputStream.bufferedReader().readText().trim().ifBlank { "-" }
        } catch (_: Exception) {
            "-"
        }
    }

    fun goRoot(root: File): String? {
        cachedGoRoot?.let { return it }
        val fromEnv = System.getenv("GOROOT")?.takeIf { it.isNotBlank() }
        if (fromEnv != null) {
            cachedGoRoot = fromEnv
            return fromEnv
        }
        val commands = listOf(listOf("go", "env", "GOROOT"), listOf("go.exe", "env", "GOROOT"))
        for (command in commands) {
            try {
                val process = ProcessBuilder(command)
                    .directory(root)
                    .redirectErrorStream(true)
                    .start()
                if (!process.waitFor(5, TimeUnit.SECONDS)) {
                    process.destroyForcibly()
                    continue
                }
                val value = process.inputStream.bufferedReader().readText().trim()
                if (value.isNotBlank()) {
                    cachedGoRoot = value
                    return value
                }
            } catch (_: Exception) {
                continue
            }
        }
        return null
    }

    fun rememberLspExecutable(command: String, root: File) {
        lastLspExecutable = command
        lastLspVersion = commandVersion(command, root)
    }

    data class ProcessResult(
        val exitCode: Int,
        val stdout: String,
        val stderr: String,
        val missingGoDoc: Boolean = false,
        val missingGo: Boolean = false,
    )
}
