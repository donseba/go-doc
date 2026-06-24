package com.donseba.godoc

import com.intellij.lang.documentation.AbstractDocumentationProvider
import com.intellij.psi.PsiElement

class GoDocDocumentationProvider : AbstractDocumentationProvider() {
    override fun generateDoc(element: PsiElement, originalElement: PsiElement?): String? {
        val source = originalElement ?: element
        val file = source.containingFile ?: return null
        val virtualFile = file.virtualFile ?: return null
        if (virtualFile.extension !in setOf("gohtml", "tmpl", "html")) return null

        val project = file.project
        val index = GoDocIndex.load(project, virtualFile.path)
        TemplateContext.typeReferenceAt(file.text, source.textOffset, index)?.let { reference ->
            val type = index.types[reference.typeName] ?: return null
            val doc = type.doc.ifBlank { "No type documentation found in the Go source." }
            return """
                <div class="definition"><b>${escape(type.name)}</b></div>
                <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
                <div class="sections"><p>${escape(type.fqName)}</p></div>
            """.trimIndent()
        }

        val contract = index.contractForFile(project, virtualFile.path) ?: return null
        val reference = TemplateContext.fieldReferenceAt(file.text, source.textOffset, index, contract) ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
        val field = owner.fields[reference.fieldName] ?: return null

        val doc = field.doc.ifBlank { "No field documentation found in the Go source." }
        return """
            <div class="definition"><b>${escape(owner.name)}.${escape(field.name)}</b> <code>${escape(field.type)}</code></div>
            <div class="content">${escape(doc).replace("\n", "<br/>")}</div>
            <div class="sections"><p>${escape(owner.fqName)}</p></div>
        """.trimIndent()
    }

    override fun getQuickNavigateInfo(element: PsiElement, originalElement: PsiElement?): String? {
        val source = originalElement ?: element
        val file = source.containingFile ?: return null
        val virtualFile = file.virtualFile ?: return null
        if (virtualFile.extension !in setOf("gohtml", "tmpl", "html")) return null

        val project = file.project
        val index = GoDocIndex.load(project, virtualFile.path)
        TemplateContext.typeReferenceAt(file.text, source.textOffset, index)?.let { reference ->
            val type = index.types[reference.typeName] ?: return null
            return "${type.name} ${type.fqName}"
        }

        val contract = index.contractForFile(project, virtualFile.path) ?: return null
        val reference = TemplateContext.fieldReferenceAt(file.text, source.textOffset, index, contract) ?: return null
        val owner = index.types[reference.ownerTypeName] ?: return null
        val field = owner.fields[reference.fieldName] ?: return null

        return "${owner.name}.${field.name} ${field.type}"
    }

    private fun escape(value: String): String {
        return value
            .replace("&", "&amp;")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
            .replace("\"", "&quot;")
    }
}
