package com.donseba.godoc

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.intellij.openapi.project.Project
import com.intellij.openapi.roots.ProjectRootManager
import com.intellij.openapi.vfs.LocalFileSystem
import java.io.File
import java.nio.charset.StandardCharsets

data class GoDocType(
    val fqName: String,
    val name: String,
    val pkg: String,
    val file: String,
    val line: Int,
    val column: Int,
    val doc: String,
    val fields: Map<String, GoDocField>,
    val methods: Map<String, GoDocMethod>,
)

data class GoDocField(
    val name: String,
    val type: String,
    val doc: String,
    val file: String,
    val line: Int,
    val column: Int,
)

data class GoDocMethod(
    val name: String,
    val type: String,
    val signature: String,
    val doc: String,
    val file: String,
    val line: Int,
    val column: Int,
)

data class GoDocFunc(
    val fqName: String,
    val name: String,
    val pkg: String,
    val file: String,
    val line: Int,
    val column: Int,
    val doc: String,
)

data class TemplateContract(
    val models: Map<String, String>,
)

class GoDocIndex(
    val types: Map<String, GoDocType>,
    val funcs: Map<String, GoDocFunc>,
    val templates: Map<String, TemplateContract>,
    val short: Map<String, List<String>>,
    val source: String? = null,
    val rootPath: String? = null,
    val checkedPaths: List<String> = emptyList(),
    val loadError: String? = null,
) {
    companion object {
        fun load(project: Project): GoDocIndex {
            return load(project, null)
        }

        fun load(project: Project, filePath: String?): GoDocIndex {
            val checked = mutableListOf<String>()
            val indexFile = findIndexFile(project, filePath, checked) ?: return empty(checkedPaths = checked)

            return try {
                parse(readIndexText(indexFile), indexFile.path, indexFile.parentFile.parentFile.path, checked)
            } catch (err: Throwable) {
                empty(
                    checkedPaths = checked,
                    loadError = "${err.javaClass.simpleName}: ${err.message ?: "failed to load index"}",
                )
            }
        }

        private fun empty(
            checkedPaths: List<String> = emptyList(),
            loadError: String? = null,
        ): GoDocIndex = GoDocIndex(
            types = emptyMap(),
            funcs = emptyMap(),
            templates = emptyMap(),
            short = emptyMap(),
            checkedPaths = checkedPaths,
            loadError = loadError,
        )

        private fun findIndexFile(project: Project, filePath: String?, checked: MutableList<String>): File? {
            if (filePath != null) {
                var dir = File(filePath).let { if (it.isDirectory) it else it.parentFile }
                while (dir != null) {
                    if (dir.name == ".go-doc") {
                        dir = dir.parentFile
                        continue
                    }
                    val goDoc = File(dir, ".go-doc/index.json")
                    checked.add(goDoc.path)
                    if (goDoc.isFile) return goDoc
                    dir = dir.parentFile
                }
            }

            val candidates = mutableListOf<File>()
            project.basePath?.let {
                candidates.add(File(it, ".go-doc/index.json"))
            }
            ProjectRootManager.getInstance(project).contentRoots.forEach { root ->
                candidates.add(File(root.path, ".go-doc/index.json"))
            }

            for (candidate in candidates.distinctBy { it.path }) {
                checked.add(candidate.path)
                if (candidate.isFile) return candidate
            }

            return null
        }

        private fun readIndexText(file: File): String {
            val bytes = file.readBytes()
            if (bytes.size >= 2) {
                val first = bytes[0].toInt() and 0xff
                val second = bytes[1].toInt() and 0xff
                if (first == 0xff && second == 0xfe) {
                    return String(bytes, StandardCharsets.UTF_16LE)
                }
                if (first == 0xfe && second == 0xff) {
                    return String(bytes, StandardCharsets.UTF_16BE)
                }
            }
            if (bytes.size >= 3) {
                val first = bytes[0].toInt() and 0xff
                val second = bytes[1].toInt() and 0xff
                val third = bytes[2].toInt() and 0xff
                if (first == 0xef && second == 0xbb && third == 0xbf) {
                    return String(bytes.copyOfRange(3, bytes.size), StandardCharsets.UTF_8)
                }
            }

            if (looksLikeUtf16Le(bytes)) {
                return String(bytes, StandardCharsets.UTF_16LE)
            }
            if (looksLikeUtf16Be(bytes)) {
                return String(bytes, StandardCharsets.UTF_16BE)
            }

            return String(bytes, StandardCharsets.UTF_8)
        }

        private fun looksLikeUtf16Le(bytes: ByteArray): Boolean {
            if (bytes.size < 4) return false
            val sample = bytes.take(80)
            val oddZeroes = sample.withIndex().count { (index, value) -> index % 2 == 1 && value.toInt() == 0 }
            return oddZeroes > sample.size / 4
        }

        private fun looksLikeUtf16Be(bytes: ByteArray): Boolean {
            if (bytes.size < 4) return false
            val sample = bytes.take(80)
            val evenZeroes = sample.withIndex().count { (index, value) -> index % 2 == 0 && value.toInt() == 0 }
            return evenZeroes > sample.size / 4
        }

        fun refreshVirtualIndex(project: Project) {
            project.basePath?.let {
                LocalFileSystem.getInstance().refreshAndFindFileByPath("$it/.go-doc/index.json")
            }
        }

        private fun parse(json: String, source: String, rootPath: String, checkedPaths: List<String>): GoDocIndex {
            val root = Gson().fromJson(json.trimStart('\uFEFF'), JsonObject::class.java)
            val types = mutableMapOf<String, GoDocType>()
            val funcs = mutableMapOf<String, GoDocFunc>()
            val templates = mutableMapOf<String, TemplateContract>()
            val short = mutableMapOf<String, List<String>>()

            jsonObject(root, "types")?.entrySet()?.forEach { (fqName, element) ->
                val obj = element.asJsonObjectOrNull() ?: return@forEach
                val fields = jsonObject(obj, "fields")?.entrySet()?.associate { (name, value) ->
                    if (value.isJsonObject) {
                        val field = value.asJsonObject
                        name to GoDocField(
                            name = name,
                            type = field.get("type")?.asString ?: "",
                            doc = field.get("doc")?.asString ?: "",
                            file = field.get("file")?.asString ?: obj.get("file")?.asString.orEmpty(),
                            line = field.get("line")?.asInt ?: 0,
                            column = field.get("column")?.asInt ?: 0,
                        )
                    } else {
                        name to GoDocField(
                            name = name,
                            type = value.asString,
                            doc = "",
                            file = obj.get("file")?.asString.orEmpty(),
                            line = 0,
                            column = 0,
                        )
                    }
                } ?: emptyMap()
                val methods = jsonObject(obj, "methods")?.entrySet()?.mapNotNull { (name, value) ->
                    val method = value.asJsonObjectOrNull() ?: return@mapNotNull null
                    name to GoDocMethod(
                        name = name,
                        type = method.get("type")?.asString ?: "",
                        signature = method.get("signature")?.asString ?: "",
                        doc = method.get("doc")?.asString ?: "",
                        file = method.get("file")?.asString ?: obj.get("file")?.asString.orEmpty(),
                        line = method.get("line")?.asInt ?: 0,
                        column = method.get("column")?.asInt ?: 0,
                    )
                }?.toMap() ?: emptyMap()
                types[fqName] = GoDocType(
                    fqName = fqName,
                    name = obj.get("name")?.asString ?: fqName.substringAfterLast('.'),
                    pkg = obj.get("package")?.asString ?: "",
                    file = obj.get("file")?.asString ?: "",
                    line = obj.get("line")?.asInt ?: 0,
                    column = obj.get("column")?.asInt ?: 0,
                    doc = obj.get("doc")?.asString ?: "",
                    fields = fields,
                    methods = methods,
                )
            }

            jsonObject(root, "templates")?.entrySet()?.forEach { (path, element) ->
                val obj = element.asJsonObjectOrNull() ?: return@forEach
                val modelsObject = jsonObject(obj, "models")
                val models = modelsObject?.entrySet()?.associate { (name, value) ->
                    name to value.asString
                } ?: emptyMap()
                templates[path] = TemplateContract(models = models)
            }

            jsonObject(root, "funcs")?.entrySet()?.forEach { (fqName, element) ->
                val obj = element.asJsonObjectOrNull() ?: return@forEach
                funcs[fqName] = GoDocFunc(
                    fqName = fqName,
                    name = obj.get("name")?.asString ?: fqName.substringAfterLast('.'),
                    pkg = obj.get("package")?.asString ?: "",
                    file = obj.get("file")?.asString ?: "",
                    line = obj.get("line")?.asInt ?: 0,
                    column = obj.get("column")?.asInt ?: 0,
                    doc = obj.get("doc")?.asString ?: "",
                )
            }

            jsonObject(root, "short")?.entrySet()?.forEach { (name, element) ->
                if (element.isJsonArray) {
                    short[name] = element.asJsonArray.mapNotNull { value -> value.asString }
                }
            }

            return GoDocIndex(
                types = types,
                funcs = funcs,
                templates = templates,
                short = short,
                source = source,
                rootPath = rootPath,
                checkedPaths = checkedPaths,
            )
        }

        private fun jsonObject(parent: JsonObject, key: String): JsonObject? {
            return parent.get(key)?.asJsonObjectOrNull()
        }

        private fun JsonElement.asJsonObjectOrNull(): JsonObject? {
            return if (isJsonObject) asJsonObject else null
        }
    }

    fun contractForFile(project: Project, filePath: String?): TemplateContract? {
        val path = filePath ?: return null
        val basePath = rootPath ?: project.basePath ?: return null
        val relative = try {
            File(basePath).toPath().relativize(File(path).toPath()).toString().replace('\\', '/')
        } catch (_: Exception) {
            path.replace('\\', '/')
        }
        val normalizedPath = path.replace('\\', '/')
        return templates[relative] ?: templates.entries.firstOrNull { (templatePath, _) ->
            relative.endsWith(templatePath) || normalizedPath.endsWith(templatePath)
        }?.value
    }

    fun fieldsForType(typeName: String?): Map<String, GoDocField> {
        return types[typeName]?.fields.orEmpty()
    }

    fun membersForType(typeName: String?): Map<String, String> {
        val type = types[typeName] ?: return emptyMap()
        return type.fields.mapValues { (_, field) -> field.type } +
            type.methods.mapValues { (_, method) -> method.type }
    }

    fun resolveExpressionType(contract: TemplateContract, expression: String, dotType: String? = null): String? {
        val valueType = resolveExpressionValueType(contract, expression, dotType) ?: return null
        return resolveGoType(valueType)
    }

    fun resolveExpressionValueType(contract: TemplateContract, expression: String, dotType: String? = null): String? {
        val clean = expression.trim()
        if (clean.isBlank()) return null

        if (clean == ".") return dotType

        val parts = clean.split('.').filter { it.isNotBlank() }
        if (parts.isEmpty()) return null

        val root = parts.first()
        val rootType = when {
            root.startsWith("_") -> contract.models[root]
            root.startsWith("$") -> contract.models[root]
            clean.startsWith(".") -> dotType
            else -> contract.models[root]
        } ?: return null

        return resolveFieldValuePath(rootType, parts.drop(1))
    }

    fun resolveFieldPath(rootType: String, fields: List<String>): String? {
        val valueType = resolveFieldValuePath(rootType, fields) ?: return null
        return resolveGoType(valueType)
    }

    fun resolveFieldValuePath(rootType: String, fields: List<String>): String? {
        var current: String? = rootType
        for ((index, field) in fields.withIndex()) {
            val typ = types[current] ?: return null
            val memberType = typ.fields[field]?.type ?: typ.methods[field]?.type ?: return null
            if (index == fields.lastIndex) return memberType
            current = resolveGoType(memberType)
        }
        return current
    }

    fun resolveGoType(typeExpr: String): String? {
        val normalized = normalizeGoType(typeExpr)
        if (types.containsKey(normalized)) return normalized
        val matches = short[normalized].orEmpty()
        return matches.singleOrNull()
    }

    fun rangeElementType(typeExpr: String?): String? {
        val normalized = stripPointer(typeExpr ?: return null)
        return when {
            normalized.startsWith("[]") -> resolveGoType(normalized.removePrefix("[]"))
            normalized.startsWith("[") -> {
                val end = normalized.indexOf(']')
                if (end == -1 || end + 1 >= normalized.length) null else resolveGoType(normalized.substring(end + 1))
            }
            normalized.startsWith("map[") -> resolveMapValueType(normalized)?.let { resolveGoType(it) }
            else -> resolveGoType(normalized)
        }
    }

    fun isRangeable(typeExpr: String?): Boolean {
        val normalized = stripPointer(typeExpr ?: return false)
        return normalized.startsWith("[]") || normalized.startsWith("[") || normalized.startsWith("map[")
    }

    private fun resolveMapValueType(typeExpr: String): String? {
        val end = typeExpr.indexOf(']')
        if (end == -1 || end + 1 >= typeExpr.length) return null
        return typeExpr.substring(end + 1)
    }

    private fun normalizeGoType(typeExpr: String): String {
        var normalized = stripPointer(typeExpr)
        while (true) {
            normalized = stripPointer(normalized)
            normalized = when {
                normalized.startsWith("[]") -> normalized.removePrefix("[]")
                normalized.startsWith("[") -> {
                    val end = normalized.indexOf(']')
                    if (end == -1 || end + 1 >= normalized.length) return normalized
                    normalized.substring(end + 1)
                }
                else -> return normalized.trim()
            }
        }
    }

    private fun stripPointer(typeExpr: String): String {
        var normalized = typeExpr.trim()
        while (normalized.startsWith("*")) {
            normalized = normalized.removePrefix("*").trim()
        }
        return normalized
    }
}
