package com.donseba.godoc

data class GoDocFieldReference(
    val ownerTypeName: String,
    val memberName: String,
)

data class GoDocTypeReference(
    val typeName: String,
)

object GoDocTemplateContext {
    fun fieldReferenceAt(
        text: String,
        offset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): GoDocFieldReference? {
        val token = tokenAt(text, offset) ?: return null
        val parts = token.split('.').filter { it.isNotBlank() }
        if (parts.isEmpty()) return null

        val rootType = when {
            token.startsWith("_") || token.startsWith("$") -> contract.accessors[parts.first()]
                ?: scopedVariablesAt(text, offset, index, contract)[parts.first()]
            token.startsWith(".") -> dotTypeAt(text, offset, index, contract)
            else -> null
        } ?: return null

        val ownerPath = if (token.startsWith(".")) {
            parts.dropLast(1)
        } else {
            parts.drop(1).dropLast(1)
        }
        val ownerType = index.resolveFieldPath(rootType, ownerPath) ?: rootType
        return GoDocFieldReference(ownerTypeName = ownerType, memberName = parts.last())
    }

    fun typeReferenceAt(text: String, offset: Int, index: GoDocIndex): GoDocTypeReference? {
        for (match in modelTypePattern.findAll(text)) {
            val range = match.groups[1]?.range ?: continue
            if (offset < range.first || offset > range.last + 1) continue
            val raw = match.groupValues[1]
            val resolved = index.resolveGoType(raw) ?: raw.takeIf { index.types.containsKey(it) } ?: return null
            return GoDocTypeReference(resolved)
        }
        return null
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
        val stack = mutableListOf(ScopeInfo(dotType = null, vars = emptyMap()))
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

    private fun tokenAt(text: String, offset: Int): String? {
        if (text.isEmpty()) return null
        var start = offset.coerceIn(0, text.length)
        var end = start
        while (start > 0 && isTokenChar(text[start - 1])) start--
        while (end < text.length && isTokenChar(text[end])) end++
        return text.substring(start, end).takeIf { it.contains('.') }
    }

    private fun isTokenChar(char: Char): Boolean {
        return char == '$' || char == '_' || char == '.' || char.isLetterOrDigit()
    }

    private data class ScopeInfo(
        val dotType: String?,
        val vars: Map<String, String>,
    )

    private val actionPattern = Regex("""\{\{\s*(?:-)?\s*(range|with|end)\b([^}]*)\}\}""")
    private val modelTypePattern = Regex("""@model\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./\-]+)""")
}
