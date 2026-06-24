package com.donseba.godoc

import com.intellij.lang.annotation.AnnotationHolder
import com.intellij.lang.annotation.Annotator
import com.intellij.lang.annotation.HighlightSeverity
import com.intellij.openapi.editor.DefaultLanguageHighlighterColors
import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.util.TextRange
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiFile

class GoDocTemplateAnnotator : Annotator {
    override fun annotate(element: PsiElement, holder: AnnotationHolder) {
        val file = element as? PsiFile ?: return
        val virtualFile = file.virtualFile ?: return
        if (!isTemplate(virtualFile.extension)) return

        val index = GoDocIndex.load(file.project, virtualFile.path)
        val contract = index.contractForFile(file.project, virtualFile.path) ?: return
        val text = file.text

        annotateParamTypes(text, index, holder)
        annotateVarTypes(text, index, holder)
        annotateRangeTypes(text, contract, index, holder)
        annotateContractAccessors(text, contract, index, holder)
        annotateDotContextFields(text, contract, index, holder)
    }

    private fun annotateParamTypes(text: String, index: GoDocIndex, holder: AnnotationHolder) {
        for (match in paramTypePattern.findAll(text)) {
            val typeName = match.groupValues[2]
            val typeRange = groupRange(match, 2)

            if (index.types.containsKey(typeName)) {
                holder
                    .newSilentAnnotation(HighlightSeverity.INFORMATION)
                    .range(typeRange)
                    .textAttributes(TYPE_KEY)
                    .create()
            } else {
                holder
                    .newAnnotation(HighlightSeverity.WARNING, "Unknown go-doc template type '$typeName'")
                    .range(typeRange)
                    .create()
            }
        }
    }

    private fun annotateVarTypes(text: String, index: GoDocIndex, holder: AnnotationHolder) {
        for (match in varTypePattern.findAll(text)) {
            val typeName = match.groupValues[2]
            val typeRange = groupRange(match, 2)

            if (index.resolveGoType(typeName) != null || index.types.containsKey(typeName)) {
                holder
                    .newSilentAnnotation(HighlightSeverity.INFORMATION)
                    .range(typeRange)
                    .textAttributes(TYPE_KEY)
                    .create()
            } else {
                holder
                    .newAnnotation(HighlightSeverity.WARNING, "Unknown go-doc template type '$typeName'")
                    .range(typeRange)
                    .create()
            }
        }
    }

    private fun annotateRangeTypes(
        text: String,
        contract: TemplateContract,
        index: GoDocIndex,
        holder: AnnotationHolder,
    ) {
        for (match in rangePattern.findAll(text)) {
            val expression = match.groupValues[1].trim()
            val sourceExpression = expression.substringAfter(":=", expression).substringAfter("=", expression).trim()
            val expressionRange = groupRange(match, 1)
            val sourceType = index.resolveExpressionValueType(contract, sourceExpression) ?: continue
            if (!index.isRangeable(sourceType)) {
                holder
                    .newAnnotation(HighlightSeverity.WARNING, "Cannot range over '$sourceExpression' because it is $sourceType")
                    .range(expressionRange)
                    .create()
            }
        }
    }

    private fun annotateContractAccessors(
        text: String,
        contract: TemplateContract,
        index: GoDocIndex,
        holder: AnnotationHolder,
    ) {
        for (match in accessorPattern.findAll(text)) {
            val accessor = match.groupValues[1]
            val field = match.groupValues[2]
            val accessorRange = groupRange(match, 1)
            val fieldRange = groupRange(match, 2)
            val typeName = contract.accessors[accessor]
                ?: contract.vars[accessor]
                ?: TemplateContext.variableTypesAt(text, match.range.first, index, contract)[accessor]

            if (typeName == null) {
                holder
                    .newAnnotation(HighlightSeverity.ERROR, "Unknown go-doc accessor '$accessor'")
                    .range(accessorRange)
                    .create()
                continue
            }

            holder
                .newSilentAnnotation(HighlightSeverity.INFORMATION)
                .range(accessorRange)
                .textAttributes(ACCESSOR_KEY)
                .create()

            val type = index.types[typeName]
            if (type == null) {
                holder
                    .newAnnotation(HighlightSeverity.WARNING, "Unknown go-doc template type '$typeName'")
                    .range(accessorRange)
                    .create()
                continue
            }

            if (!type.fields.containsKey(field)) {
                val builder = holder
                    .newAnnotation(HighlightSeverity.ERROR, "Unknown field '$field' on ${type.name}")
                    .range(fieldRange)
                nearestField(field, type.fields.keys)?.let { suggestion ->
                    builder.withFix(ReplaceTextQuickFix(fieldRange, suggestion, "Replace with '$suggestion'"))
                }
                builder.create()
                continue
            }

            holder
                .newSilentAnnotation(HighlightSeverity.INFORMATION)
                .range(fieldRange)
                .textAttributes(FIELD_KEY)
                .create()
        }
    }

    private fun annotateDotContextFields(
        text: String,
        contract: TemplateContract,
        index: GoDocIndex,
        holder: AnnotationHolder,
    ) {
        for (match in dotFieldPattern.findAll(text)) {
            val chain = match.value
            val dotType = TemplateContext.dotTypeAt(text, match.range.first, index, contract) ?: continue
            val fields = chain.split('.').filter { it.isNotBlank() }
            if (fields.isEmpty()) continue

            val ownerType = index.resolveFieldPath(dotType, fields.dropLast(1)) ?: continue
            val owner = index.types[ownerType] ?: continue
            val field = fields.last()
            val fieldStart = match.range.last - field.length + 1
            val fieldRange = TextRange(fieldStart, fieldStart + field.length)

            if (!owner.fields.containsKey(field)) {
                val builder = holder
                    .newAnnotation(HighlightSeverity.ERROR, "Unknown field '$field' on ${owner.name}")
                    .range(fieldRange)
                nearestField(field, owner.fields.keys)?.let { suggestion ->
                    builder.withFix(ReplaceTextQuickFix(fieldRange, suggestion, "Replace with '$suggestion'"))
                }
                builder.create()
                continue
            }

            holder
                .newSilentAnnotation(HighlightSeverity.INFORMATION)
                .range(fieldRange)
                .textAttributes(FIELD_KEY)
                .create()
        }
    }

    private fun groupRange(match: MatchResult, group: Int): TextRange {
        val range = match.groups[group]?.range ?: match.range
        return TextRange(range.first, range.last + 1)
    }

    private fun isTemplate(extension: String?): Boolean {
        return extension in setOf("gohtml", "tmpl", "html")
    }

    private fun nearestField(value: String, candidates: Set<String>): String? {
        return candidates
            .map { it to levenshtein(value.lowercase(), it.lowercase()) }
            .filter { (_, distance) -> distance <= 2 }
            .minByOrNull { (_, distance) -> distance }
            ?.first
    }

    private fun levenshtein(left: String, right: String): Int {
        if (left == right) return 0
        if (left.isEmpty()) return right.length
        if (right.isEmpty()) return left.length

        var previous = IntArray(right.length + 1) { it }
        var current = IntArray(right.length + 1)

        for (i in left.indices) {
            current[0] = i + 1
            for (j in right.indices) {
                val cost = if (left[i] == right[j]) 0 else 1
                current[j + 1] = minOf(
                    current[j] + 1,
                    previous[j + 1] + 1,
                    previous[j] + cost,
                )
            }
            val swap = previous
            previous = current
            current = swap
        }

        return previous[right.length]
    }

    private companion object {
        val ACCESSOR_KEY: TextAttributesKey = TextAttributesKey.createTextAttributesKey(
            "GO_DOC_ACCESSOR",
            DefaultLanguageHighlighterColors.LOCAL_VARIABLE,
        )
        val FIELD_KEY: TextAttributesKey = TextAttributesKey.createTextAttributesKey(
            "GO_DOC_FIELD",
            DefaultLanguageHighlighterColors.INSTANCE_FIELD,
        )
        val TYPE_KEY: TextAttributesKey = TextAttributesKey.createTextAttributesKey(
            "GO_DOC_TYPE",
            DefaultLanguageHighlighterColors.CLASS_NAME,
        )

        val accessorPattern = Regex("""([_\u0024][A-Za-z][A-Za-z0-9_]*)\.([A-Za-z][A-Za-z0-9_]*)""")
        val dotFieldPattern = Regex("""(?<![\u0024A-Za-z0-9_])(?:\.[A-Za-z][A-Za-z0-9_]*)+""")
        val paramTypePattern = Regex("""@param\s+([A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z0-9_./\-]+)""")
        val varTypePattern = Regex("""@var\s+(\$?[A-Za-z][A-Za-z0-9_]*)\s+([A-Za-z0-9_./\-]+)""")
        val rangePattern = Regex("""\{\{\s*(?:-)?\s*range\s+([^}]*)\}\}""")
    }
}
