package com.donseba.godoc

data class GoDocFieldReference(
    val ownerTypeName: String,
    val memberName: String,
    val startOffset: Int,
    val endOffset: Int,
)

data class GoDocTypeReference(
    val typeName: String,
    val startOffset: Int,
    val endOffset: Int,
)

data class GoDocFuncReference(
    val funcName: String,
    val startOffset: Int,
    val endOffset: Int,
)

data class GoDocTemplateIncludeReference(
    val templateName: String,
    val templatePath: String,
    val startOffset: Int,
    val endOffset: Int,
)

object GoDocTemplateContext {
    fun fieldReferenceAt(
        text: String,
        offset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): GoDocFieldReference? {
        val token = tokenAt(text, offset) ?: return null
        val parts = token.text.split('.').filter { it.isNotBlank() }
        if (parts.isEmpty()) return null

        val scopedModel = token.text.startsWith("_.") && parts.firstOrNull() == "_"
        val parenthesizedRoot = if (token.text.startsWith(".")) {
            parenthesizedRootBefore(text, token.startOffset, index, contract)
        } else {
            null
        }
        val rootType = when {
            parenthesizedRoot != null -> parenthesizedRoot
            scopedModel -> parts.getOrNull(1)?.let { contract.models[it] }
            token.text.startsWith("$") -> contract.models[parts.first()]
                ?: scopedVariablesAt(text, offset, index, contract)[parts.first()]
            token.text.startsWith(".") -> dotTypeAt(text, offset, index, contract)
            else -> contract.models[parts.first()]
                ?: index.resolveExpressionType(contract, parts.first())
        } ?: return null

        val fieldName = parts.last()
        val memberStart = token.endOffset - fieldName.length
        val rootOnlyReference = parts.size == 1 && !token.text.startsWith(".")
        if (rootOnlyReference) {
            return GoDocFieldReference(
                ownerTypeName = rootType,
                memberName = parts.first(),
                startOffset = token.startOffset,
                endOffset = token.endOffset,
            )
        }

        val ownerPath = if (token.text.startsWith(".")) {
            parts.dropLast(1)
        } else if (scopedModel) {
            parts.drop(2).dropLast(1)
        } else {
            parts.drop(1).dropLast(1)
        }
        val ownerType = index.resolveFieldPath(rootType, ownerPath) ?: rootType
        return GoDocFieldReference(
            ownerTypeName = ownerType,
            memberName = fieldName,
            startOffset = memberStart,
            endOffset = token.endOffset,
        )
    }

    private fun parenthesizedRootBefore(
        text: String,
        dotStart: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): String? {
        if (dotStart <= 0 || dotStart > text.length || text[dotStart - 1] != ')') return null
        val open = matchingOpenParen(text, dotStart - 1)
        if (open < 0) return null
        val actionStart = text.substring(0, open).lastIndexOf("{{")
        val actionEnd = text.substring(0, open).lastIndexOf("}}")
        if (actionStart < 0 || actionEnd > actionStart) return null
        val inner = text.substring(open + 1, dotStart - 1).trim()
        if (inner.isBlank()) return null
        return index.resolveExpressionType(contract, inner)
    }

    private fun matchingOpenParen(text: String, close: Int): Int {
        if (close !in text.indices || text[close] != ')') return -1
        var depth = 0
        var inQuote = false
        var escaped = false
        for (index in close downTo 0) {
            val ch = text[index]
            when {
                escaped -> escaped = false
                ch == '\\' -> escaped = true
                ch == '"' -> inQuote = !inQuote
                inQuote -> Unit
                ch == ')' -> depth++
                ch == '(' -> {
                    depth--
                    if (depth == 0) return index
                }
            }
        }
        return -1
    }

    fun typeReferenceAt(text: String, offset: Int, index: GoDocIndex): GoDocTypeReference? {
        for (match in typeMatches(text)) {
            val range = match.groups[1]?.range ?: continue
            if (offset < range.first || offset > range.last + 1) continue
            val raw = match.groupValues[1]
            val resolved = index.resolveGoType(raw) ?: raw.takeIf { index.types.containsKey(it) } ?: return null
            val shortStart = range.first + raw.length - raw.substringAfterLast('.').length
            if (offset < shortStart) return null
            return GoDocTypeReference(resolved, shortStart, range.last + 1)
        }
        return null
    }

    fun templateFunctionAt(
        text: String,
        offset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): GoDocFuncReference? {
        for (match in templateActionPattern.findAll(text)) {
            val actionRange = match.range
            if (offset < actionRange.first || offset > actionRange.last + 1) continue
            templateFunctionReferencesInAction(match.value, actionRange.first, index, contract)
                .firstOrNull { offset >= it.startOffset && offset <= it.endOffset }
                ?.let { return it }
        }
        return null
    }

    fun templateIncludeAt(
        text: String,
        offset: Int,
        index: GoDocIndex,
    ): GoDocTemplateIncludeReference? {
        for (match in templateActionPattern.findAll(text)) {
            val actionRange = match.range
            if (offset < actionRange.first || offset > actionRange.last + 1) continue
            return templateIncludeReferencesInAction(match.value, actionRange.first, index)
                .firstOrNull { offset >= it.startOffset && offset <= it.endOffset }
        }
        return null
    }

    private fun templateIncludeReferencesInAction(
        actionText: String,
        actionStartOffset: Int,
        index: GoDocIndex,
    ): List<GoDocTemplateIncludeReference> {
        val match = templateIncludePattern.find(actionText) ?: return emptyList()
        val nameRange = match.groups[1]?.range ?: return emptyList()
        val name = match.groupValues[1]
        val template = index.templateByName(name) ?: return emptyList()
        return listOf(
            GoDocTemplateIncludeReference(
                templateName = name,
                templatePath = template.first,
                startOffset = actionStartOffset + nameRange.first,
                endOffset = actionStartOffset + nameRange.last + 1,
            ),
        )
    }

    private fun templateFunctionReferencesInAction(
        actionText: String,
        actionStartOffset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): List<GoDocFuncReference> {
        val open = actionText.indexOf("{{")
        if (open < 0) return emptyList()
        val close = actionText.lastIndexOf("}}")
        if (close < open) return emptyList()

        val refs = mutableListOf<GoDocFuncReference>()
        var cursor = open + 2
        while (cursor < close) {
            val char = actionText[cursor]
            if (inQuotedString(actionText, cursor) || !isTokenChar(char) || char == '.' || char == '$') {
                cursor++
                continue
            }

            val tokenStart = cursor
            while (cursor < close && isTokenChar(actionText[cursor])) {
                cursor++
            }
            val name = actionText.substring(tokenStart, cursor)
            val fnName = contract.funcs[name] ?: continue
            if (!index.funcs.containsKey(fnName)) continue
            refs.add(
                GoDocFuncReference(
                    funcName = fnName,
                    startOffset = actionStartOffset + tokenStart,
                    endOffset = actionStartOffset + cursor,
                ),
            )
        }
        return refs
    }

    fun fieldReferencesInRange(
        text: String,
        startOffset: Int,
        endOffset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): List<GoDocFieldReference> {
        return tokenPattern.findAll(text, startOffset)
            .takeWhile { it.range.first < endOffset }
            .mapNotNull { match ->
                if (inQuotedString(text, match.range.first)) return@mapNotNull null
                val token = match.value
                if (!token.contains('.') && contract.models[token] == null) return@mapNotNull null
                val middle = match.range.first + token.length / 2
                fieldReferenceAt(text, middle, index, contract)
            }
            .filter { it.startOffset < endOffset && it.endOffset > startOffset }
            .toList()
    }

    fun typeReferencesInRange(text: String, startOffset: Int, endOffset: Int, index: GoDocIndex): List<GoDocTypeReference> {
        return typeMatches(text, startOffset).asSequence()
            .takeWhile { (it.groups[1]?.range?.first ?: Int.MAX_VALUE) < endOffset }
            .mapNotNull { match ->
                val range = match.groups[1]?.range ?: return@mapNotNull null
                if (range.last + 1 <= startOffset || range.first >= endOffset) return@mapNotNull null
                val raw = match.groupValues[1]
                val resolved = index.resolveGoType(raw) ?: raw.takeIf { index.types.containsKey(it) } ?: return@mapNotNull null
                val shortStart = range.first + raw.length - raw.substringAfterLast('.').length
                if (shortStart >= endOffset || range.last + 1 <= startOffset) return@mapNotNull null
                GoDocTypeReference(resolved, shortStart, range.last + 1)
            }
            .toList()
    }

    private fun typeMatches(text: String, startOffset: Int = 0): List<MatchResult> {
        return typePatterns
            .flatMap { it.findAll(text, startOffset) }
            .sortedBy { it.groups[1]?.range?.first ?: Int.MAX_VALUE }
    }

    fun funcReferencesInRange(text: String, startOffset: Int, endOffset: Int, index: GoDocIndex): List<GoDocFuncReference> {
        val refs = mutableListOf<GoDocFuncReference>()
        refs.addAll(funcTypePattern.findAll(text, startOffset)
            .takeWhile { (it.groups[1]?.range?.first ?: Int.MAX_VALUE) < endOffset }
            .mapNotNull { match ->
                val range = match.groups[1]?.range ?: return@mapNotNull null
                if (range.last + 1 <= startOffset || range.first >= endOffset) return@mapNotNull null
                val raw = match.groupValues[1]
                val normalized = normalizeType(raw)
                if (!index.funcs.containsKey(normalized)) return@mapNotNull null
                val shortStart = range.first + raw.length - raw.substringAfterLast('.').length
                GoDocFuncReference(normalized, shortStart, range.last + 1)
            })

        val contract = contractForText(text, index)
        if (contract != null) {
            for (match in templateActionPattern.findAll(text, startOffset)) {
                val range = match.range
                if (range.first >= endOffset) break
                if (range.last + 1 <= startOffset) continue
                for (ref in templateFunctionReferencesInAction(match.value, range.first, index, contract)) {
                    if (ref.startOffset < endOffset && ref.endOffset > startOffset) {
                        refs.add(ref)
                    }
                }
            }
        }
        return refs.distinctBy { "${it.startOffset}:${it.endOffset}:${it.funcName}" }
    }

    fun templateIncludeReferencesInRange(
        text: String,
        startOffset: Int,
        endOffset: Int,
        index: GoDocIndex,
    ): List<GoDocTemplateIncludeReference> {
        return templateActionPattern.findAll(text, startOffset)
            .takeWhile { it.range.first < endOffset }
            .flatMap { match -> templateIncludeReferencesInAction(match.value, match.range.first, index) }
            .filter { it.startOffset < endOffset && it.endOffset > startOffset }
            .distinctBy { "${it.startOffset}:${it.endOffset}:${it.templatePath}" }
            .toList()
    }

    private fun contractForText(text: String, index: GoDocIndex): TemplateContract? {
        val models = modelContractPattern.findAll(text).associate { match ->
            val name = match.groupValues[1]
            val rawType = match.groupValues[2]
            name to (index.resolveGoType(rawType) ?: rawType)
        }
        val dot = dotContractPattern.find(text)?.let { match ->
            val rawType = normalizeType(match.groupValues[1])
            index.resolveGoType(rawType) ?: rawType
        }.orEmpty()
        val funcs = funcContractPattern.findAll(text).associate { match ->
            match.groupValues[1] to normalizeType(match.groupValues[2])
        }
        if (models.isEmpty() && dot.isEmpty() && funcs.isEmpty()) return null
        return TemplateContract(models = models, dot = dot, funcs = funcs)
    }

    private fun dotTypeAt(text: String, offset: Int, index: GoDocIndex, contract: TemplateContract): String? {
        return scopeAt(text, offset, index, contract).dotType
    }

    private fun scopedVariablesAt(
        text: String,
        offset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): Map<String, String> {
        return scopeAt(text, offset, index, contract).vars
    }

    private fun scopeAt(text: String, offset: Int, index: GoDocIndex, contract: TemplateContract): ScopeInfo {
        val stack = mutableListOf(ScopeInfo(dotType = contract.dot.ifBlank { null }, vars = emptyMap()))
        val beforeCaret = text.substring(0, offset.coerceIn(0, text.length))

        for (match in actionPattern.findAll(beforeCaret)) {
            val keyword = match.groupValues[1]
            val expression = match.groupValues[2].trim()
            val parent = stack.last()
            when (keyword) {
                "range" -> {
                    val sourceType = index.resolveExpressionValueType(contract, sourceExpression(expression), parent.dotType)
                    val elementType = index.rangeElementType(sourceType)
                    stack.add(
                        ScopeInfo(
                            dotType = elementType,
                            vars = parent.vars + rangeVariables(expression, elementType),
                        ),
                    )
                }
                "with" -> {
                    stack.add(
                        ScopeInfo(
                            dotType = index.resolveExpressionType(contract, sourceExpression(expression), parent.dotType),
                            vars = parent.vars,
                        ),
                    )
                }
                "end" -> {
                    if (stack.size > 1) stack.removeAt(stack.lastIndex)
                }
            }
        }

        return stack.last()
    }

    private fun sourceExpression(expression: String): String {
        return expression.substringAfter(":=", expression).substringAfter("=", expression).trim()
    }

    private fun rangeVariables(expression: String, elementType: String?): Map<String, String> {
        if (elementType == null || !expression.contains(":=")) return emptyMap()
        val names = expression.substringBefore(":=").split(',').map { it.trim() }
        val valueName = names.lastOrNull { it.startsWith("$") } ?: return emptyMap()
        return mapOf(valueName to elementType)
    }

    private fun tokenAt(text: String, offset: Int): Token? {
        if (text.isEmpty()) return null
        var start = offset.coerceIn(0, text.length)
        var end = start
        while (start > 0 && isTokenChar(text[start - 1])) start--
        while (end < text.length && isTokenChar(text[end])) end++
        val token = text.substring(start, end)
        return Token(token, start, end).takeIf { token.contains('.') || token in modelNamesNear(text, start) }
    }

    private fun isTokenChar(char: Char): Boolean {
        return char == '$' || char == '_' || char == '.' || char.isLetterOrDigit()
    }

    private fun inQuotedString(text: String, offset: Int): Boolean {
        var inQuote = false
        var escaped = false
        for (index in 0 until offset.coerceIn(0, text.length)) {
            val char = text[index]
            when {
                escaped -> escaped = false
                char == '\\' -> escaped = true
                char == '"' -> inQuote = !inQuote
            }
        }
        return inQuote
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

    private data class ScopeInfo(
        val dotType: String?,
        val vars: Map<String, String>,
    )

    private data class Token(
        val text: String,
        val startOffset: Int,
        val endOffset: Int,
    )

    private fun modelNamesNear(text: String, offset: Int): Set<String> {
        val before = text.substring(0, offset.coerceIn(0, text.length))
        return modelDeclarationPattern.findAll(before).map { it.groupValues[1] }.toSet()
    }

    private val actionPattern = Regex("""\{\{\s*(?:-)?\s*(range|with|end)\b([^}]*)\}\}""")
    private val templateActionPattern = Regex("""\{\{\s*(?:-)?\s*[^}]*\}\}""")
    private val templateIncludePattern = Regex("""^\{\{\s*(?:-)?\s*template\s+"([^"]+)"(?:\s+[^}]*)?\s*-?\}\}$""")
    private val modelContractPattern = Regex("""(?m)^\s*@model\s+([\u0024A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z0-9_./\-]+)""")
    private val dotContractPattern = Regex("""(?m)^\s*@dot\s+([A-Za-z0-9_./\-]+)""")
    private val funcContractPattern = Regex("""(?m)^\s*@func\s+([\u0024A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z0-9_./\-]+)""")
    private val modelTypePattern = Regex("""@model\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./\-]+)""")
    private val dotTypePattern = Regex("""@dot\s+([A-Za-z0-9_./\-]+)""")
    private val typePatterns = listOf(modelTypePattern, dotTypePattern)
    private val funcTypePattern = Regex("""@func\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./\-]+)""")
    private val modelDeclarationPattern = Regex("""@model\s+([\u0024A-Za-z][A-Za-z0-9_]*)\s+[A-Za-z0-9_./\-]+""")
    private val tokenPattern = Regex("""[\u0024_A-Za-z][\u0024_A-Za-z0-9]*(?:\.[A-Za-z][A-Za-z0-9_]*)*|\.[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*""")
}
