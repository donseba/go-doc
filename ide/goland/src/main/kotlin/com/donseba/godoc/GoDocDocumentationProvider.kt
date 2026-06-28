package com.donseba.godoc

import com.intellij.lang.documentation.AbstractDocumentationProvider
import com.intellij.psi.PsiElement

class GoDocDocumentationProvider : AbstractDocumentationProvider() {
    override fun generateDoc(element: PsiElement, originalElement: PsiElement?): String? {
        val source = originalElement ?: element
        val file = source.containingFile ?: return null
        val virtualFile = file.virtualFile ?: return null
        if (!isSupportedTemplate(virtualFile)) return null

        val project = file.project
        val index = GoDocIndex.load(project, virtualFile.path)
        GoDocTemplateContext.templateIncludeAt(file.text, source.textOffset, index)?.let { reference ->
            val child = index.templates[reference.templatePath]
            val expected = child?.dot?.let { index.resolveGoType(it) ?: it }.orEmpty()
            val expectedLabel = index.types[expected]?.name ?: expected.substringAfterLast('.').ifBlank { "template data" }
            return """
                <div class="definition"><b>template</b> <code>${escape(reference.templateName)}</code></div>
                <div class="content">Expects <code>${escape(expectedLabel)}</code>.</div>
                <div class="sections"><p>${escape(reference.templatePath)}</p></div>
            """.trimIndent()
        }

        val hoverOffsets = hoverOffsets(source, file.text)
        hoverOffsets.firstNotNullOfOrNull { offset ->
            GoDocTemplateContext.typedRootReferenceAt(file.text, offset, index)
        }?.let { reference ->
            val type = index.types[reference.typeName] ?: return null
            val doc = type.doc.ifBlank { "No type documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(type.name)}</b> <code>${escape(type.fqName)}</code></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
            """.trimIndent()
        }

        val contract = hoverOffsets
            .firstNotNullOfOrNull { offset -> index.contractForFileAt(project, virtualFile.path, offset) }
            ?: return null
        hoverOffsets.firstNotNullOfOrNull { offset ->
            GoDocTemplateContext.templateFunctionAt(file.text, offset, index, contract)
        }?.let { reference ->
            val fn = index.funcs[reference.funcName] ?: return null
            val signature = fn.signature.ifBlank { "func ${fn.name}" }
            val doc = fn.doc.ifBlank { "No function documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(fn.name)}</b> <code>${escape(signature)}</code></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
                <div class="sections"><p>${escape(fn.fqName)}</p></div>
            """.trimIndent()
        }

        val reference = hoverOffsets.firstNotNullOfOrNull { offset ->
            GoDocTemplateContext.fieldReferenceAt(file.text, offset, index, contract)
        } ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
        if (contract.isTypedRoot(reference.memberName, reference.ownerTypeName)) {
            val doc = owner.doc.ifBlank { "No type documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(owner.name)}</b> <code>${escape(owner.fqName)}</code></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
            """.trimIndent()
        }
        owner.fields[reference.memberName]?.let { field ->
            val doc = field.doc.ifBlank { "No field documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(owner.name)}.${escape(field.name)}</b> <code>${escape(field.type)}</code></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
                <div class="sections"><p>${escape(owner.fqName)}</p></div>
            """.trimIndent()
        }
        owner.methods[reference.memberName]?.let { method ->
            val signature = method.signature.ifBlank { "func() ${method.type}".trim() }
            val doc = method.doc.ifBlank { "No method documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(owner.name)}.${escape(method.name)}</b> <code>${escape(signature)}</code></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
                <div class="sections"><p>${escape(owner.fqName)}</p></div>
            """.trimIndent()
        }

        return null
    }

    override fun getQuickNavigateInfo(element: PsiElement, originalElement: PsiElement?): String? {
        val source = originalElement ?: element
        val file = source.containingFile ?: return null
        val virtualFile = file.virtualFile ?: return null
        if (!isSupportedTemplate(virtualFile)) return null

        val project = file.project
        val index = GoDocIndex.load(project, virtualFile.path)
        GoDocTemplateContext.templateIncludeAt(file.text, source.textOffset, index)?.let { reference ->
            val child = index.templates[reference.templatePath]
            val expected = child?.dot?.let { index.resolveGoType(it) ?: it }.orEmpty()
            val expectedLabel = index.types[expected]?.name ?: expected.substringAfterLast('.').ifBlank { "template data" }
            return "template ${reference.templateName} expects $expectedLabel"
        }

        val hoverOffsets = hoverOffsets(source, file.text)
        hoverOffsets.firstNotNullOfOrNull { offset ->
            GoDocTemplateContext.typedRootReferenceAt(file.text, offset, index)
        }?.let { reference ->
            val type = index.types[reference.typeName] ?: return null
            return "${type.name} ${type.fqName}".trim()
        }

        val contract = hoverOffsets
            .firstNotNullOfOrNull { offset -> index.contractForFileAt(project, virtualFile.path, offset) }
            ?: return null
        hoverOffsets.firstNotNullOfOrNull { offset ->
            GoDocTemplateContext.templateFunctionAt(file.text, offset, index, contract)
        }?.let { reference ->
            val fn = index.funcs[reference.funcName] ?: return null
            return "${fn.name} ${fn.signature.ifBlank { fn.fqName }}".trim()
        }

        val reference = hoverOffsets.firstNotNullOfOrNull { offset ->
            GoDocTemplateContext.fieldReferenceAt(file.text, offset, index, contract)
        } ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
        if (contract.isTypedRoot(reference.memberName, reference.ownerTypeName)) {
            return "${owner.name} ${owner.fqName}".trim()
        }
        owner.fields[reference.memberName]?.let { field ->
            return "${owner.name}.${field.name} ${field.type}"
        }
        owner.methods[reference.memberName]?.let { method ->
            return "${owner.name}.${method.name} ${method.type}".trim()
        }
        return null
    }

    private fun escape(value: String): String {
        return value
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
            .replace("\"", "&quot;")
    }

    private fun hoverOffsets(element: PsiElement, text: String): List<Int> {
        val start = element.textRange?.startOffset ?: element.textOffset
        val end = element.textRange?.endOffset ?: element.textOffset
        val offsets = mutableListOf(
            element.textOffset,
            start,
            end - 1,
            element.textOffset - 1,
            element.textOffset + 1,
        )
        var tokenStart = start.coerceIn(0, text.length)
        var tokenEnd = end.coerceIn(tokenStart, text.length)
        while (tokenStart > 0 && isTemplateTokenChar(text[tokenStart - 1])) tokenStart--
        while (tokenEnd < text.length && isTemplateTokenChar(text[tokenEnd])) tokenEnd++
        offsets.add(tokenStart)
        offsets.add(tokenEnd - 1)
        offsets.add((tokenStart + tokenEnd) / 2)
        return offsets
            .filter { it >= 0 && it <= text.length }
            .distinct()
    }

    private fun isTemplateTokenChar(char: Char): Boolean {
        return char == '$' || char == '_' || char == '.' || char.isLetterOrDigit()
    }
}
