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

data class GoDocTypedRootReference(
    val typeName: String,
    val startOffset: Int,
    val endOffset: Int,
)

data class GoDocTemplateIncludeReference(
    val templateName: String,
    val templatePath: String,
    val targetPath: String,
    val targetLine: Int,
    val targetColumn: Int,
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
        val token = tokenAt(text, offset, contract) ?: return null
        val boundedOffset = offset.coerceIn(0, text.length)
        val references = fieldReferencesForToken(text, token, index, contract)
        return references.firstOrNull { boundedOffset >= it.startOffset && boundedOffset <= it.endOffset }
            ?: references.firstOrNull { boundedOffset + 1 == it.startOffset && text.getOrNull(boundedOffset) == '.' }
            ?: references.firstOrNull { boundedOffset == it.startOffset - 1 && text.getOrNull(boundedOffset) == '.' }
            ?: references.firstOrNull { boundedOffset == it.endOffset }
    }

    private fun fieldReferencesForToken(
        text: String,
        token: Token,
        index: GoDocIndex,
        contract: TemplateContract,
    ): List<GoDocFieldReference> {
        val ranges = tokenIdentifierRanges(token)
        if (ranges.isEmpty()) return emptyList()

        val scopeOffset = token.startOffset
        var rootType: String?
        var memberRanges = ranges
        val references = mutableListOf<GoDocFieldReference>()

        if (token.text.startsWith(".")) {
            rootType = parenthesizedRootBefore(text, token.startOffset, index, contract)
                ?: dotTypeAt(text, scopeOffset, index, contract)
        } else {
            val rootName = text.substring(ranges.first().first, ranges.first().last + 1)
            rootType = contract.typedRootType(rootName)
                ?: scopedVariablesAt(text, scopeOffset, index, contract)[rootName]
                ?: index.resolveExpressionType(contract, rootName)
            if (rootType != null && contract.isTypedRoot(rootName, rootType)) {
                references.add(
                    GoDocFieldReference(
                        ownerTypeName = rootType,
                        memberName = rootName,
                        startOffset = ranges.first().first,
                        endOffset = ranges.first().last + 1,
                    ),
                )
            }
            memberRanges = ranges.drop(1)
        }
        if (rootType == null || memberRanges.isEmpty()) return references

        var ownerType = index.resolveGoType(rootType) ?: rootType
        for (range in memberRanges) {
            val name = text.substring(range.first, range.last + 1)
            references.add(
                GoDocFieldReference(
                    ownerTypeName = ownerType,
                    memberName = name,
                    startOffset = range.first,
                    endOffset = range.last + 1,
                ),
            )
            val memberType = index.membersForType(ownerType)[name] ?: return references
            ownerType = index.resolveGoType(memberType) ?: memberType
        }
        return references
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
        typedRootReferences(text, index)
            .firstOrNull { offset >= it.startOffset && offset <= it.endOffset }
            ?.let { return GoDocTypeReference(it.typeName, it.startOffset, it.endOffset) }
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
                targetPath = template.second.source.ifBlank { template.first },
                targetLine = template.second.line.takeIf { it > 0 } ?: 1,
                targetColumn = template.second.column.takeIf { it > 0 } ?: 1,
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
        val scan = expandedTokenRange(text, startOffset, endOffset)
        return tokenPattern.findAll(text, scan.first)
            .takeWhile { it.range.first < scan.last }
            .flatMap { match ->
                if (inTemplateComment(text, match.range.first)) return@flatMap emptyList()
                if (inQuotedString(text, match.range.first)) return@flatMap emptyList()
                val token = match.value
                if (!token.contains('.') && contract.typedRootType(token) == null) return@flatMap emptyList()
                fieldReferencesForToken(
                    text,
                    Token(token, match.range.first, match.range.last + 1),
                    index,
                    contract,
                )
            }
            .filter { it.startOffset < endOffset && it.endOffset > startOffset }
            .distinctBy { "${it.startOffset}:${it.endOffset}:${it.ownerTypeName}:${it.memberName}" }
            .toList()
    }

    fun typedRootReferencesInRange(text: String, startOffset: Int, endOffset: Int, index: GoDocIndex): List<GoDocTypedRootReference> {
        return typedRootReferences(text, index)
            .asSequence()
            .filter { it.startOffset < endOffset && it.endOffset > startOffset }
            .distinctBy { "${it.startOffset}:${it.endOffset}:${it.typeName}" }
            .toList()
    }

    fun typedRootReferenceAt(text: String, offset: Int, index: GoDocIndex): GoDocTypedRootReference? {
        val boundedOffset = offset.coerceIn(0, text.length)
        return typedRootReferences(text, index)
            .firstOrNull { boundedOffset >= it.startOffset && boundedOffset <= it.endOffset }
            ?: typedRootReferences(text, index)
                .firstOrNull { boundedOffset == it.startOffset - 1 || boundedOffset == it.endOffset }
    }

    private fun typedRootReferences(text: String, index: GoDocIndex): List<GoDocTypedRootReference> {
        return annotationContractPattern.findAll(contractScanTextWithOffsets(text))
            .flatMap { match ->
                val annotation = match.groupValues[1]
                val nameRange = match.groups[2]?.range ?: return@flatMap emptyList()
                val explicitTypeRange = match.groups[3]?.range
                val typeName = when (annotation) {
                    "model", "symbol" -> match.groupValues.getOrNull(3).orEmpty()
                    else -> {
                        if (isReservedContractAnnotation(annotation)) return@flatMap emptyList()
                        val explicit = match.groupValues.getOrNull(3).orEmpty()
                        val aliasType = index.symbolAliases[annotation]
                        if (aliasType == null && index.symbolStrictMode) ""
                        else explicit.ifBlank { aliasType.orEmpty() }
                    }
                }
                if (typeName.isBlank()) return@flatMap emptyList()
                val resolved = index.resolveGoType(typeName) ?: typeName.takeIf { index.types.containsKey(it) } ?: return@flatMap emptyList()
                val refs = mutableListOf<GoDocTypedRootReference>()
                refs.add(GoDocTypedRootReference(resolved, nameRange.first, nameRange.last + 1))
                explicitTypeRange?.let { typeRange ->
                    val raw = match.groupValues[3]
                    val shortStart = typeRange.first + raw.length - raw.substringAfterLast('.').length
                    refs.add(GoDocTypedRootReference(resolved, shortStart, typeRange.last + 1))
                }
                refs
            }
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
        val contractText = contractScanText(text)
        val typedRoots = parseTypedRoots(contractText, index)
        val models = typedRoots
            .filter { it.annotation == "model" }
            .associate { it.name to (index.resolveGoType(it.typeName) ?: it.typeName) }
        val dot = dotContractPattern.find(contractText)?.let { match ->
            val rawType = normalizeType(match.groupValues[1])
            index.resolveGoType(rawType) ?: rawType
        }.orEmpty()
        val funcs = funcContractPattern.findAll(contractText).associate { match ->
            match.groupValues[1] to normalizeType(match.groupValues[2])
        }
        val gens = genContractPattern.findAll(contractText).associate { match ->
            match.groupValues[1] to match.groupValues[2]
        }
        val symbols = typedRoots
            .filter { it.annotation != "model" }
            .associate { it.name to (index.resolveGoType(it.typeName) ?: it.typeName) }
        if (models.isEmpty() && dot.isEmpty() && funcs.isEmpty() && gens.isEmpty() && symbols.isEmpty()) return null
        val generatedModels = gens.keys.mapNotNull { name ->
            val typeName = "$GEN_TYPE_PREFIX$name"
            if (index.types.containsKey(typeName)) name to typeName else null
        }.toMap()
        return TemplateContract(models = models + generatedModels, dot = dot, funcs = funcs, gens = gens, symbols = symbols)
    }

    private fun parseTypedRoots(text: String, index: GoDocIndex): List<TypedRoot> {
        return annotationContractPattern.findAll(text).mapNotNull { match ->
            val annotation = match.groupValues[1]
            val name = match.groupValues[2]
            var typeName = match.groupValues.getOrNull(3).orEmpty()
            when (annotation) {
                "model", "symbol" -> {
                    if (typeName.isBlank()) return@mapNotNull null
                }
                else -> {
                    if (isReservedContractAnnotation(annotation)) return@mapNotNull null
                    val aliasType = index.symbolAliases[annotation]
                    if (aliasType == null && index.symbolStrictMode) return@mapNotNull null
                    if (typeName.isBlank() && aliasType != null) typeName = aliasType
                    if (typeName.isBlank()) return@mapNotNull null
                }
            }
            TypedRoot(annotation = annotation, name = name, typeName = normalizeType(typeName))
        }.toList()
    }

    private fun contractScanText(text: String): String {
        val matches = templateCommentBodyPattern.findAll(text).map { it.groupValues[1].trim() }.filter { it.isNotBlank() }.toList()
        if (matches.isEmpty()) return text
        return matches.joinToString(separator = "\n", postfix = "\n")
    }

    private fun contractScanTextWithOffsets(text: String): String {
        val chars = CharArray(text.length) { ' ' }
        for (match in templateCommentBodyPattern.findAll(text)) {
            val body = match.groups[1] ?: continue
            for (index in body.range) {
                chars[index] = text[index]
            }
        }
        return String(chars)
    }

    private fun inTemplateComment(text: String, offset: Int): Boolean {
        val open = text.lastIndexOf("{{/*", offset)
        if (open < 0) return false
        val close = text.lastIndexOf("*/}}", offset)
        return close < open
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

    private fun tokenAt(text: String, offset: Int, contract: TemplateContract): Token? {
        if (text.isEmpty()) return null
        var start = offset.coerceIn(0, text.length)
        var end = start
        while (start > 0 && isTokenChar(text[start - 1])) start--
        while (end < text.length && isTokenChar(text[end])) end++
        val token = text.substring(start, end)
        return Token(token, start, end).takeIf {
            token.contains('.') || contract.typedRootType(token) != null || token in modelNamesNear(text, start)
        }
    }

    private fun expandedTokenRange(text: String, startOffset: Int, endOffset: Int): IntRange {
        var start = startOffset.coerceIn(0, text.length)
        var end = endOffset.coerceIn(start, text.length)
        while (start > 0 && isTokenChar(text[start - 1])) {
            start--
        }
        while (end < text.length && isTokenChar(text[end])) {
            end++
        }
        return start until end
    }

    private fun tokenIdentifierRanges(token: Token): List<IntRange> {
        val ranges = mutableListOf<IntRange>()
        var index = 0
        while (index < token.text.length) {
            while (index < token.text.length && (token.text[index] == '.' || !isIdentifierStart(token.text[index]))) {
                index++
            }
            val start = index
            while (index < token.text.length && isIdentifierPart(token.text[index])) {
                index++
            }
            if (start < index) {
                ranges.add((token.startOffset + start) until (token.startOffset + index))
            }
        }
        return ranges
    }

    private fun isIdentifierStart(char: Char): Boolean {
        return char == '$' || char == '_' || char.isLetter()
    }

    private fun isIdentifierPart(char: Char): Boolean {
        return isIdentifierStart(char) || char.isDigit()
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

    private data class TypedRoot(
        val annotation: String,
        val name: String,
        val typeName: String,
    )

    private fun isReservedContractAnnotation(name: String): Boolean {
        return name == "dot" || name == "func" || name == "gen"
    }

    private fun modelNamesNear(text: String, offset: Int): Set<String> {
        val before = text.substring(0, offset.coerceIn(0, text.length))
        return modelDeclarationPattern.findAll(before).map { it.groupValues[1] }.toSet()
    }

    private val actionPattern = Regex("""\{\{\s*(?:-)?\s*(range|with|end)\b([^}]*)\}\}""")
    private val templateActionPattern = Regex("""\{\{\s*(?:-)?\s*[^}]*\}\}""")
    private val templateIncludePattern = Regex("""^\{\{\s*(?:-)?\s*(?:template|block)\s+"([^"]+)"(?:\s+[^}]*)?\s*-?\}\}$""")
    private val dotContractPattern = Regex("""(?m)^\s*@dot\s+([A-Za-z0-9_./\-]+)""")
    private val funcContractPattern = Regex("""(?m)^\s*@func\s+([\u0024A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z0-9_./\-]+)""")
    private val genContractPattern = Regex("""(?m)^\s*@gen\s+([A-Za-z_][A-Za-z0-9_]*)\s+([A-Za-z0-9_./\-]+)""")
    private val annotationContractPattern = Regex("""(?m)^\s*@([A-Za-z_][A-Za-z0-9_]*)\s+([\u0024A-Za-z_][A-Za-z0-9_]*)(?:\s+([A-Za-z0-9_./\[\]*\-]+))?""")
    private val modelTypePattern = Regex("""@model\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./\-]+)""")
    private val dotTypePattern = Regex("""@dot\s+([A-Za-z0-9_./\-]+)""")
    private val typePatterns = listOf(modelTypePattern, dotTypePattern)
    private val funcTypePattern = Regex("""@func\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./\-]+)""")
    private val modelDeclarationPattern = Regex("""@model\s+([\u0024A-Za-z][A-Za-z0-9_]*)\s+[A-Za-z0-9_./\-]+""")
    private val templateCommentBodyPattern = Regex("""(?s)\{\{/\*(.*?)\*/\}\}""")
    private val tokenPattern = Regex("""[\u0024_A-Za-z][\u0024_A-Za-z0-9]*(?:\.[A-Za-z][A-Za-z0-9_]*)*|\.[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*)*""")
    private const val GEN_TYPE_PREFIX = "\$go-doc/gen."
}
