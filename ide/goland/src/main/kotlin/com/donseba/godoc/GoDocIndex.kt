package com.donseba.godoc

import com.google.gson.Gson
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.intellij.openapi.fileEditor.FileDocumentManager
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
    val result: String,
    val signature: String,
    val params: List<String>,
)

data class TemplateContract(
    val name: String = "",
    val roots: Map<String, String>,
    val dot: String = "",
    val funcs: Map<String, String> = emptyMap(),
    val gens: Map<String, String> = emptyMap(),
    val source: String = "",
    val line: Int = 0,
    val column: Int = 0,
) {
    fun typedRootType(name: String): String? = roots[name]

    fun isTypedRoot(name: String, typeName: String): Boolean = typedRootType(name) == typeName
}

class GoDocIndex(
    val types: Map<String, GoDocType>,
    val funcs: Map<String, GoDocFunc>,
    val templates: Map<String, TemplateContract>,
    val short: Map<String, List<String>>,
    val symbolAliases: Map<String, String> = emptyMap(),
    val symbolStrictMode: Boolean = false,
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
            val root = GoDocIndexer.findModuleRoot(filePath) ?: goDocReadAction { project.basePath }?.let { File(it) }
            if (root != null && !GoDocIndexer.enabled(project, root)) {
                return empty(checkedPaths = checked)
            }
            val indexFile = if (root != null && GoDocIndexer.autoIndexEnabled(project, root)) {
                findIndexFile(project, filePath, checked)
            } else {
                null
            }
                ?: root?.let { shadowIndexFile(it, checked) }
                ?: run {
                    if (root != null) GoDocIndexer.requestShadowIndex(project, root)
                    return empty(checkedPaths = checked)
                }

            return try {
                parse(readIndexText(indexFile), indexFile.path, root?.path ?: indexFile.parentFile.parentFile.path, checked)
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
            symbolAliases = emptyMap(),
            symbolStrictMode = false,
            checkedPaths = checkedPaths,
            loadError = loadError,
        )

        private fun shadowIndexFile(root: File, checked: MutableList<String>): File? {
            val file = GoDocIndexer.shadowIndexFile(root)
            checked.add(file.path)
            return file.takeIf { it.isFile }
        }

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
            goDocReadAction {
                project.basePath?.let {
                    candidates.add(File(it, ".go-doc/index.json"))
                }
                ProjectRootManager.getInstance(project).contentRoots.forEach { root ->
                    candidates.add(File(root.path, ".go-doc/index.json"))
                }
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
            goDocReadAction { project.basePath }?.let {
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
                val rootsObject = jsonObject(obj, "roots")
                val roots = rootsObject?.entrySet()?.associate { (name, value) ->
                    name to value.asString
                } ?: emptyMap()
                val funcsObject = jsonObject(obj, "funcs")
                val templateFuncs = funcsObject?.entrySet()?.associate { (name, value) ->
                    name to value.asString
                } ?: emptyMap()
                val gensObject = jsonObject(obj, "gens")
                val gens = gensObject?.entrySet()?.associate { (name, value) ->
                    name to value.asString
                } ?: emptyMap()
                templates[path] = TemplateContract(
                    name = obj.get("name")?.asString ?: "",
                    roots = roots,
                    dot = obj.get("dot")?.asString ?: "",
                    funcs = templateFuncs,
                    gens = gens,
                    source = obj.get("source")?.asString ?: "",
                    line = obj.get("line")?.asInt ?: 0,
                    column = obj.get("column")?.asInt ?: 0,
                )
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
                    result = obj.get("result")?.asString ?: "",
                    signature = obj.get("signature")?.asString ?: "",
                    params = obj.get("params")?.asJsonArray?.mapNotNull { it.asString } ?: emptyList(),
                )
            }

            jsonObject(root, "short")?.entrySet()?.forEach { (name, element) ->
                if (element.isJsonArray) {
                    short[name] = element.asJsonArray.mapNotNull { value -> value.asString }
                }
            }
            val symbolAliases = jsonObject(root, "symbolAliases")?.entrySet()?.associate { (name, value) ->
                name to value.asString
            } ?: emptyMap()
            val symbolStrictMode = root.get("symbolStrictMode")?.asBoolean ?: false

            return GoDocIndex(
                types = types,
                funcs = funcs,
                templates = templates,
                short = short,
                symbolAliases = symbolAliases,
                symbolStrictMode = symbolStrictMode,
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
        val text = templateText(path)
        return contractForFileText(project, path, text, null)
    }

    fun contractForFileAt(project: Project, filePath: String?, offset: Int): TemplateContract? {
        val path = filePath ?: return null
        val text = templateText(path)
        return contractForFileText(project, path, text, offset)
    }

    private fun contractForFileText(project: Project, path: String, text: String?, offset: Int?): TemplateContract? {
        val basePath = rootPath ?: goDocReadAction { project.basePath } ?: return null
        val relative = try {
            File(basePath).toPath().relativize(File(path).toPath()).toString().replace('\\', '/')
        } catch (_: Exception) {
            path.replace('\\', '/')
        }
        val normalizedPath = path.replace('\\', '/')
        val base = templates[relative] ?: templates.entries.firstOrNull { (templatePath, _) ->
            relative.endsWith(templatePath) || normalizedPath.endsWith(templatePath)
        }?.value
        val activeBase = offset
            ?.let { activeDefineNameAt(text.orEmpty(), it) }
            ?.let { defineName -> templateByNameInFile(relative, defineName)?.second }
            ?: base
        return mergeInlineContract(path, activeBase)
    }

    fun templateByName(name: String): Pair<String, TemplateContract>? {
        val normalized = name.trimStart('/').replace('\\', '/')
        val entry = templates.entries.firstOrNull { (path, _) ->
            val templatePath = path.replace('\\', '/')
            templatePath == normalized ||
                templatePath.substringAfterLast('/') == normalized ||
                templatePath.endsWith("/$normalized")
        } ?: templates.entries.firstOrNull { (_, contract) ->
            contract.name == normalized
        }
        return entry?.let { it.key to it.value }
    }

    private fun templateByNameInFile(relative: String, name: String): Pair<String, TemplateContract>? {
        val normalized = relative.replace('\\', '/')
        return templates.entries.firstOrNull { (_, contract) ->
            contract.name == name && contract.source.replace('\\', '/') == normalized
        }?.let { it.key to it.value } ?: templateByName(name)
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
        parenthesizedExpressionPath(clean)?.let { (inner, path) ->
            val rootType = resolveExpressionValueType(contract, inner, dotType) ?: return null
            return resolveFieldValuePath(rootType, path)
        }
        resolveFunctionCommandValueType(contract, clean)?.let { return it }

        val parts = clean.split('.').filter { it.isNotBlank() }
        if (parts.isEmpty()) return null

        val root = parts.first()
        val path: List<String>
        val rootType = when {
            root == "_" -> {
                path = parts.drop(2)
                parts.getOrNull(1)?.let { contract.roots[it] }
            }
            root.startsWith("$") -> {
                path = parts.drop(1)
                contract.roots[root]
            }
            clean.startsWith(".") -> {
                path = parts
                dotType
            }
            else -> {
                path = parts.drop(1)
                contract.typedRootType(root) ?: functionResultValueType(contract, root)
            }
        } ?: return null

        return resolveFieldValuePath(rootType, path)
    }

    private fun resolveFunctionCommandValueType(contract: TemplateContract, expression: String): String? {
        val fields = expression.split(Regex("""\s+""")).filter { it.isNotBlank() }
        if (fields.isEmpty()) return null
        if (fields.size == 1 && fields.first().contains('.')) return null
        return functionResultValueType(contract, fields.first())
    }

    private fun parenthesizedExpressionPath(expression: String): Pair<String, List<String>>? {
        val clean = expression.trim()
        if (!clean.startsWith("(")) return null
        val close = matchingCloseParen(clean, 0)
        if (close < 0 || close + 1 >= clean.length || clean[close + 1] != '.') return null
        val path = clean.substring(close + 1).split('.').filter { it.isNotBlank() }
        if (path.isEmpty()) return null
        return clean.substring(1, close).trim() to path
    }

    private fun matchingCloseParen(text: String, open: Int): Int {
        if (open !in text.indices || text[open] != '(') return -1
        var depth = 0
        var inQuote = false
        var escaped = false
        for (index in open until text.length) {
            val ch = text[index]
            when {
                escaped -> escaped = false
                ch == '\\' -> escaped = true
                ch == '"' -> inQuote = !inQuote
                inQuote -> Unit
                ch == '(' -> depth++
                ch == ')' -> {
                    depth--
                    if (depth == 0) return index
                }
            }
        }
        return -1
    }

    private fun functionResultValueType(contract: TemplateContract, name: String): String? {
        val fnName = contract.funcs[name] ?: return null
        val result = funcs[fnName]?.result?.takeIf { it.isNotBlank() } ?: return null
        if (isCompositeValueType(result)) return result
        return resolveGoType(result) ?: result
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

    private fun isCompositeValueType(typeExpr: String): Boolean {
        val normalized = stripPointer(typeExpr)
        return normalized.startsWith("[]") || normalized.startsWith("[") || normalized.startsWith("map[")
    }

    private fun normalizeType(typeExpr: String): String {
        val lastSlash = typeExpr.lastIndexOf('/')
        val lastDot = typeExpr.lastIndexOf('.')
        return if (lastSlash > lastDot) {
            typeExpr.substring(0, lastSlash) + "." + typeExpr.substring(lastSlash + 1)
        } else {
            typeExpr
        }
    }

    private fun mergeInlineContract(filePath: String, base: TemplateContract?): TemplateContract? {
        val rawText = templateText(filePath) ?: return base
        val text = contractScanText(contractAnnotationText(rawText, base))
        val typedRoots = parseTypedRoots(text)
        val dotMatch = dotPattern.find(text)
        val funcMatches = funcPattern.findAll(text).toList()
        val genMatches = genPattern.findAll(text).toList()
        if (
            typedRoots.isEmpty() &&
            dotMatch == null &&
            funcMatches.isEmpty() &&
            genMatches.isEmpty()
        ) return base

        val roots = if (typedRoots.isEmpty()) base?.roots.orEmpty().toMutableMap() else typedRoots.associate { root ->
            root.name to (resolveGoType(root.typeName) ?: root.typeName)
        }.toMutableMap()
        val dot = dotMatch?.let { match ->
            val rawType = normalizeType(match.groupValues[1])
            resolveGoType(rawType) ?: rawType
        } ?: base?.dot.orEmpty()
        val funcs = base?.funcs.orEmpty() + funcMatches.associate { match ->
            match.groupValues[1] to normalizeType(match.groupValues[2])
        }
        val gens = (base?.gens.orEmpty() + genMatches.associate { match ->
            match.groupValues[1] to match.groupValues[2]
        })
        for ((name, _) in gens) {
            val generatedType = "$GEN_TYPE_PREFIX$name"
            if (types.containsKey(generatedType)) {
                roots[name] = generatedType
            }
        }
        return TemplateContract(roots = roots, dot = dot, funcs = funcs, gens = gens)
    }

    private fun parseTypedRoots(text: String): List<TypedRoot> {
        return annotationPattern.findAll(text).mapNotNull { match ->
            val annotation = match.groupValues[1]
            val name = match.groupValues[2]
            var typeName = match.groupValues.getOrNull(3).orEmpty()
            if (isReservedContractAnnotation(annotation)) return@mapNotNull null
            val aliasType = symbolAliases[annotation]
            if (aliasType == null && symbolStrictMode) return@mapNotNull null
            if (typeName.isBlank() && aliasType != null) typeName = aliasType
            if (typeName.isBlank()) return@mapNotNull null
            TypedRoot(annotation = annotation, name = name, typeName = typeName)
        }.toList()
    }

    private fun contractAnnotationText(text: String, base: TemplateContract?): String {
        val name = base?.name.orEmpty()
        if (name.isNotBlank()) {
            return defineBodyText(text, name) ?: text
        }
        return topLevelTemplateText(text)
    }

    private fun topLevelTemplateText(text: String): String {
        val out = StringBuilder()
        var cursor = 0
        for (block in defineBlocks(text)) {
            if (block.start > cursor) out.append(text.substring(cursor, block.start))
            cursor = maxOf(cursor, block.end)
        }
        if (cursor < text.length) out.append(text.substring(cursor))
        return out.toString()
    }

    private fun defineBodyText(text: String, name: String): String? {
        return defineBlocks(text).firstOrNull { it.name == name }?.let { text.substring(it.bodyStart, it.bodyEnd) }
    }

    private fun activeDefineNameAt(text: String, offset: Int): String? {
        val boundedOffset = offset.coerceIn(0, text.length)
        val stack = mutableListOf<String>()
        for (match in templateActionPattern.findAll(text.substring(0, boundedOffset))) {
            val content = actionContent(match.value) ?: continue
            val define = defineActionPattern.matchEntire(content)
            if (define != null) {
                stack.add(define.groupValues[1])
                continue
            }
            if (content == "end" && stack.isNotEmpty()) stack.removeAt(stack.lastIndex)
        }
        return stack.lastOrNull()
    }

    private fun defineBlocks(text: String): List<DefineBlock> {
        data class OpenDefine(val name: String, val start: Int, val bodyStart: Int)
        val stack = mutableListOf<OpenDefine>()
        val blocks = mutableListOf<DefineBlock>()
        for (match in templateActionPattern.findAll(text)) {
            val content = actionContent(match.value) ?: continue
            val define = defineActionPattern.matchEntire(content)
            if (define != null) {
                stack.add(OpenDefine(define.groupValues[1], match.range.first, match.range.last + 1))
                continue
            }
            if (content == "end" && stack.isNotEmpty()) {
                val open = stack.removeAt(stack.lastIndex)
                blocks.add(DefineBlock(open.name, open.start, match.range.last + 1, open.bodyStart, match.range.first))
            }
        }
        return blocks.sortedBy { it.start }
    }

    private fun actionContent(action: String): String? {
        val start = action.indexOf("{{")
        val end = action.lastIndexOf("}}")
        if (start < 0 || end < start) return null
        return action.substring(start + 2, end).trim().trim('-').trim()
    }

    private fun templateText(filePath: String): String? {
        val documentText = goDocReadAction {
            val virtualFile = LocalFileSystem.getInstance().findFileByIoFile(File(filePath))
            if (virtualFile != null) {
                FileDocumentManager.getInstance().getDocument(virtualFile)?.let { document ->
                    return@goDocReadAction document.text
                }
            }
            null
        }
        if (documentText != null) return documentText
        return try {
            File(filePath).readText()
        } catch (_: Exception) {
            null
        }
    }

    private fun contractScanText(text: String): String {
        val matches = templateCommentPattern.findAll(text).map { it.groupValues[1].trim() }.filter { it.isNotBlank() }.toList()
        if (matches.isEmpty()) return text
        return matches.joinToString(separator = "\n", postfix = "\n")
    }

    private val dotPattern = Regex("""(?m)^\s*@dot\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*)\s*$""")
    private val funcPattern = Regex("""(?m)^\s*@func\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./-]*)\s*$""")
    private val genPattern = Regex("""(?m)^\s*@gen\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_./-]*)\s*$""")
    private val annotationPattern = Regex("""(?m)^\s*@([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z_][A-Za-z0-9_]*)(?:\s+([A-Za-z_][A-Za-z0-9_./\[\]*-]*))?\s*$""")
    private val templateCommentPattern = Regex("""(?s)\{\{/\*(.*?)\*/\}\}""")
    private val templateActionPattern = Regex("""\{\{\s*(?:-)?\s*[^}]*\}\}""")
    private val defineActionPattern = Regex("define\\s+\"([^\"]+)\"")
    private val GEN_TYPE_PREFIX = "\$go-doc/gen."

    private data class DefineBlock(
        val name: String,
        val start: Int,
        val end: Int,
        val bodyStart: Int,
        val bodyEnd: Int,
    )

    private data class TypedRoot(
        val annotation: String,
        val name: String,
        val typeName: String,
    )

    private fun isReservedContractAnnotation(name: String): Boolean {
        return name == "dot" || name == "func" || name == "gen"
    }
}
