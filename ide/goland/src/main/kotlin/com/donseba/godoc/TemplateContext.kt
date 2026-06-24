package com.donseba.godoc

data class FieldTarget(
    val typeName: String,
    val typedPrefix: String,
)

data class FieldReference(
    val ownerTypeName: String,
    val fieldName: String,
)

data class TypeReference(
    val typeName: String,
)

object TemplateContext {
    fun dotTypeAt(text: String, offset: Int, index: GoDocIndex, contract: TemplateContract): String? {
        return scopeAt(text, offset, index, contract).dotType
    }

    fun variableTypesAt(text: String, offset: Int, index: GoDocIndex, contract: TemplateContract): Map<String, String> {
        return scopeAt(text, offset, index, contract).vars
    }

    fun fieldTargetBeforeCaret(
        text: String,
        offset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): FieldTarget? {
        val cleanText = text
            .replace("IntellijIdeaRulezzz", "")
            .replace("DummyIdentifier", "")
        val beforeCaret = cleanText.substring(0, offset.coerceIn(0, cleanText.length)).takeLast(500).trimEnd()
        val token = tokenBeforeCaret.find(beforeCaret)?.value ?: return null
        val lastDot = token.lastIndexOf('.')
        if (lastDot == -1) return null

        val chain = token.substring(0, lastDot + 1)
        val typedPrefix = token.substring(lastDot + 1)
        val parts = chain.split('.').filter { it.isNotBlank() }

        if (chain.startsWith("_") || chain.startsWith("$")) {
            if (parts.isEmpty()) return null
            val root = parts.first()
            val rootType = contract.accessors[root]
                ?: contract.vars[root]
                ?: variableTypesAt(text, offset, index, contract)[root]
                ?: return null
            val typeName = index.resolveFieldPath(rootType, parts.drop(1)) ?: return null
            return FieldTarget(typeName = typeName, typedPrefix = typedPrefix)
        }

        if (chain.startsWith(".")) {
            val dotType = dotTypeAt(text, offset, index, contract) ?: return null
            val typeName = index.resolveFieldPath(dotType, parts) ?: return null
            return FieldTarget(typeName = typeName, typedPrefix = typedPrefix)
        }

        return null
    }

    fun fieldReferenceAt(
        text: String,
        offset: Int,
        index: GoDocIndex,
        contract: TemplateContract,
    ): FieldReference? {
        val token = tokenAt(text, offset) ?: return null
        val parts = token.split('.').filter { it.isNotBlank() }
        if (parts.isEmpty()) return null

        if (token.startsWith("_") || token.startsWith("$")) {
            val rootType = contract.accessors[parts.first()]
                ?: contract.vars[parts.first()]
                ?: variableTypesAt(text, offset, index, contract)[parts.first()]
                ?: return null
            val ownerType = index.resolveFieldPath(rootType, parts.drop(1).dropLast(1)) ?: return null
            return FieldReference(ownerTypeName = ownerType, fieldName = parts.last())
        }

        if (token.startsWith(".")) {
            val dotType = dotTypeAt(text, offset, index, contract) ?: return null
            val ownerType = index.resolveFieldPath(dotType, parts.dropLast(1)) ?: return null
            return FieldReference(ownerTypeName = ownerType, fieldName = parts.last())
        }

        return null
    }

    fun typeReferenceAt(text: String, offset: Int, index: GoDocIndex): TypeReference? {
        val typeName = contractTypeTokenAt(text, offset) ?: return null
        val resolved = index.resolveGoType(typeName) ?: typeName.takeIf { index.types.containsKey(it) } ?: return null
        return TypeReference(resolved)
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
                    val source = rangeSourceExpression(expression)
                    val sourceType = index.resolveExpressionValueType(contract, source, parent.dotType)
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
                            dotType = index.resolveExpressionType(contract, assignmentSourceExpression(expression), parent.dotType),
                            vars = parent.vars + assignedVariable(expression, index, contract, parent.dotType),
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

    private fun rangeSourceExpression(expression: String): String {
        return expression.substringAfter(":=", expression).substringAfter("=", expression).trim()
    }

    private fun assignmentSourceExpression(expression: String): String {
        return expression.substringAfter(":=", expression).substringAfter("=", expression).trim()
    }

    private fun rangeVariables(expression: String, elementType: String?): Map<String, String> {
        if (elementType == null || !expression.contains(":=")) return emptyMap()
        val declaration = expression.substringBefore(":=")
        val names = declaration.split(',').map { it.trim() }.filter { it.startsWith("$") }
        val valueName = names.lastOrNull() ?: return emptyMap()
        return mapOf(valueName to elementType)
    }

    private fun assignedVariable(
        expression: String,
        index: GoDocIndex,
        contract: TemplateContract,
        dotType: String?,
    ): Map<String, String> {
        if (!expression.contains(":=")) return emptyMap()
        val name = expression.substringBefore(":=").trim()
        if (!name.startsWith("$") || name.contains(",")) return emptyMap()
        val sourceType = index.resolveExpressionType(contract, assignmentSourceExpression(expression), dotType) ?: return emptyMap()
        return mapOf(name to sourceType)
    }

    private fun tokenAt(text: String, offset: Int): String? {
        if (text.isEmpty()) return null
        var start = offset.coerceIn(0, text.length)
        var end = start
        while (start > 0 && isTokenChar(text[start - 1])) start--
        while (end < text.length && isTokenChar(text[end])) end++
        return text.substring(start, end).takeIf { it.contains('.') }
    }

    private fun contractTypeTokenAt(text: String, offset: Int): String? {
        for (match in contractTypePattern.findAll(text)) {
            val range = match.groups[2]?.range ?: continue
            if (offset < range.first || offset > range.last + 1) continue
            return match.groupValues[2]
        }
        return null
    }

    private fun isTokenChar(char: Char): Boolean {
        return char == '$' || char == '_' || char == '.' || char.isLetterOrDigit()
    }

    private data class ScopeInfo(
        val dotType: String?,
        val vars: Map<String, String>,
    )

    private val actionPattern = Regex("""\{\{\s*(?:-)?\s*(range|with|end)\b([^}]*)\}\}""")
    private val contractTypePattern = Regex("""@(param|var)\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+([A-Za-z0-9_./\-]+)""")
    private val tokenBeforeCaret = Regex("[\\u0024A-Za-z0-9_.]+\\z")
}
