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
        val contract = index.contractForFile(project, virtualFile.path) ?: return null
        GoDocTemplateContext.templateFunctionAt(file.text, source.textOffset, index, contract)?.let { reference ->
            val fn = index.funcs[reference.funcName] ?: return null
            val signature = fn.signature.ifBlank { "func ${fn.name}" }
            val doc = fn.doc.ifBlank { "No function documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(fn.name)}</b> <code>${escape(signature)}</code></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
                <div class="sections"><p>${escape(fn.fqName)}</p></div>
            """.trimIndent()
        }

        val reference = GoDocTemplateContext.fieldReferenceAt(file.text, source.textOffset, index, contract) ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
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
        val contract = index.contractForFile(project, virtualFile.path) ?: return null
        GoDocTemplateContext.templateFunctionAt(file.text, source.textOffset, index, contract)?.let { reference ->
            val fn = index.funcs[reference.funcName] ?: return null
            return "${fn.name} ${fn.signature.ifBlank { fn.fqName }}".trim()
        }

        val reference = GoDocTemplateContext.fieldReferenceAt(file.text, source.textOffset, index, contract) ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
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
}
