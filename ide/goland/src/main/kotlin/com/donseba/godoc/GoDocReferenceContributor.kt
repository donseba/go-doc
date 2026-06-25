package com.donseba.godoc

import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.TextRange
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.patterns.PlatformPatterns
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiManager
import com.intellij.psi.PsiReference
import com.intellij.psi.PsiReferenceBase
import com.intellij.psi.PsiReferenceContributor
import com.intellij.psi.PsiReferenceProvider
import com.intellij.psi.PsiReferenceRegistrar
import com.intellij.util.ProcessingContext
import java.io.File

class GoDocReferenceContributor : PsiReferenceContributor() {
    override fun registerReferenceProviders(registrar: PsiReferenceRegistrar) {
        registrar.registerReferenceProvider(
            PlatformPatterns.psiElement(),
            object : PsiReferenceProvider() {
                override fun getReferencesByElement(element: PsiElement, context: ProcessingContext): Array<PsiReference> {
                    val file = element.containingFile ?: return PsiReference.EMPTY_ARRAY
                    val virtualFile = file.virtualFile ?: return PsiReference.EMPTY_ARRAY
                    if (!isSupportedTemplate(virtualFile)) return PsiReference.EMPTY_ARRAY

                    val project = file.project
                    val index = GoDocIndex.load(project, virtualFile.path)
                    val elementRange = element.textRange ?: return PsiReference.EMPTY_ARRAY
                    val references = mutableListOf<PsiReference>()

                    GoDocTemplateContext.typeReferencesInRange(file.text, elementRange.startOffset, elementRange.endOffset, index)
                        .forEach { ref ->
                            val type = index.types[ref.typeName] ?: return@forEach
                            references.add(
                                GoDocPsiReference(
                                    element = element,
                                    absoluteStart = ref.startOffset,
                                    absoluteEnd = ref.endOffset,
                                    target = GoDocTarget(index.rootPath, type.file, type.line, type.column),
                                ),
                            )
                        }

                    val contract = index.contractForFile(project, virtualFile.path) ?: return references.toTypedArray()
                    GoDocTemplateContext.fieldReferencesInRange(file.text, elementRange.startOffset, elementRange.endOffset, index, contract)
                        .forEach { ref ->
                            val owner = index.types[ref.ownerTypeName] ?: return@forEach
                            val target = when {
                                contract.models[ref.memberName] == ref.ownerTypeName -> GoDocTarget(index.rootPath, owner.file, owner.line, owner.column)
                                owner.fields[ref.memberName] != null -> {
                                    val field = owner.fields.getValue(ref.memberName)
                                    GoDocTarget(index.rootPath, field.file.ifBlank { owner.file }, field.line, field.column)
                                }
                                owner.methods[ref.memberName] != null -> {
                                    val method = owner.methods.getValue(ref.memberName)
                                    GoDocTarget(index.rootPath, method.file.ifBlank { owner.file }, method.line, method.column)
                                }
                                else -> null
                            } ?: return@forEach
                            references.add(
                                GoDocPsiReference(
                                    element = element,
                                    absoluteStart = ref.startOffset,
                                    absoluteEnd = ref.endOffset,
                                    target = target,
                                ),
                            )
                        }

                    return references.toTypedArray()
                }
            },
        )
    }
}

private class GoDocPsiReference(
    element: PsiElement,
    absoluteStart: Int,
    absoluteEnd: Int,
    private val target: GoDocTarget,
) : PsiReferenceBase<PsiElement>(
    element,
    TextRange(
        (absoluteStart - element.textRange.startOffset).coerceAtLeast(0),
        (absoluteEnd - element.textRange.startOffset).coerceAtMost(element.textLength),
    ),
    true,
) {
    override fun resolve(): PsiElement? {
        return targetElement(element.project, target)
    }
}

private data class GoDocTarget(
    val root: String?,
    val file: String,
    val line: Int,
    val column: Int,
)

private fun targetElement(project: Project, target: GoDocTarget): PsiElement? {
    if (target.file.isBlank()) return null
    val root = target.root ?: project.basePath ?: return null
    val targetVirtualFile = LocalFileSystem.getInstance()
        .findFileByIoFile(File(root, target.file)) ?: return null
    val targetPsiFile = PsiManager.getInstance(project).findFile(targetVirtualFile) ?: return null
    val document = FileDocumentManager.getInstance().getDocument(targetVirtualFile) ?: return targetPsiFile
    val line = (target.line - 1).coerceAtLeast(0)
    val column = (target.column - 1).coerceAtLeast(0)
    val targetOffset = if (line < document.lineCount) {
        (document.getLineStartOffset(line) + column).coerceAtMost(document.getLineEndOffset(line))
    } else {
        0
    }
    return targetPsiFile.findElementAt(targetOffset) ?: targetPsiFile
}
