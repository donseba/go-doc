package com.donseba.godoc

import com.intellij.codeInsight.completion.CompletionContributor
import com.intellij.codeInsight.completion.CompletionParameters
import com.intellij.codeInsight.completion.CompletionResultSet
import com.intellij.codeInsight.lookup.LookupElementBuilder
import com.intellij.openapi.vfs.VirtualFile
import kotlin.math.max
import kotlin.math.min

class GoDocCompletionContributor : CompletionContributor() {
    override fun fillCompletionVariants(parameters: CompletionParameters, result: CompletionResultSet) {
        val file = parameters.originalFile.virtualFile ?: return
        if (!isTemplate(file)) return

        val prefix = textBeforeCaret(parameters)
        val index = GoDocIndex.load(parameters.position.project, file.path)
        val contract = index.contractForFile(parameters.position.project, file.path)

        if (isParamTypePosition(prefix)) {
            addTypeCompletions(index, result)
            return
        }

        if (contract == null) return

        val target = resolveTarget(parameters, index, contract) ?: return
        val type = index.types[target.typeName] ?: return
        val fieldResult = result.withPrefixMatcher(target.typedPrefix)

        for ((field, fieldInfo) in type.fields.toSortedMap()) {
            fieldResult.addElement(
                LookupElementBuilder
                    .create(field)
                    .withTypeText(fieldInfo.type, true)
                    .withTailText(" ${type.name}", true)
                    .withBoldness(fieldInfo.doc.isNotBlank())
                    .withLookupString(fieldInfo.doc)
            )
        }
    }

    private fun addTypeCompletions(index: GoDocIndex, result: CompletionResultSet) {
        for (type in index.types.values.sortedWith(compareBy<GoDocType> { it.name }.thenBy { it.fqName })) {
            result.addElement(
                LookupElementBuilder
                    .create(type.fqName)
                    .withPresentableText(type.name)
                    .withTailText(" ${type.pkg}", true)
                    .withTypeText(type.file, true)
            )
        }
    }

    private fun isTemplate(file: VirtualFile): Boolean {
        return file.extension in setOf("gohtml", "tmpl", "html")
    }

    private fun textBeforeCaret(parameters: CompletionParameters): String {
        val text = parameters.originalFile.text
        val offset = parameters.offset.coerceIn(0, text.length)
        return text.substring(0, offset).takeLast(300)
    }

    private fun isParamTypePosition(prefix: String): Boolean {
        return Regex("""@(param|var)\s+[\u0024A-Za-z][A-Za-z0-9_]*\s+[A-Za-z0-9_./\-]*$""").containsMatchIn(prefix)
    }

    private fun resolveTarget(
        parameters: CompletionParameters,
        index: GoDocIndex,
        contract: TemplateContract,
    ): FieldTarget? {
        val text = parameters.originalFile.text
        val offsets = listOf(
            parameters.offset,
            parameters.position.textOffset,
            parameters.originalPosition?.textOffset ?: -1,
        )
            .filter { it >= 0 }
            .flatMap { offset -> listOf(offset, offset - 1, offset + 1) }
            .map { offset -> min(max(offset, 0), text.length) }
            .distinct()

        for (offset in offsets) {
            TemplateContext.fieldTargetBeforeCaret(text, offset, index, contract)?.let { return it }
        }
        return null
    }
}
