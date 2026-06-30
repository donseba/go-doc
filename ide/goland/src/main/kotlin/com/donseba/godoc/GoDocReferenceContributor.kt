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
                    return goDocReadAction {
                        val file = element.containingFile ?: return@goDocReadAction PsiReference.EMPTY_ARRAY
                        val virtualFile = file.virtualFile ?: return@goDocReadAction PsiReference.EMPTY_ARRAY
                        if (!isSupportedTemplate(virtualFile)) return@goDocReadAction PsiReference.EMPTY_ARRAY

                        val project = file.project
                        val index = GoDocIndex.load(project, virtualFile.path)
                        val elementRange = element.textRange ?: return@goDocReadAction PsiReference.EMPTY_ARRAY
                        val templateRange = goDocTemplateActionRange(element, file.text)
                        val scanStart = templateRange?.first ?: elementRange.startOffset
                        val scanEnd = templateRange?.second ?: elementRange.endOffset
                        val references = mutableListOf<PsiReference>()

                        GoDocTemplateContext.typedRootReferencesInRange(file.text, elementRange.startOffset, elementRange.endOffset, index)
                            .forEach { ref ->
                                if (!elementOwnsToken(element, ref.startOffset, ref.endOffset)) return@forEach
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

                        GoDocTemplateContext.typeReferencesInRange(file.text, elementRange.startOffset, elementRange.endOffset, index)
                            .forEach { ref ->
                                if (!elementOwnsToken(element, ref.startOffset, ref.endOffset)) return@forEach
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

                        GoDocTemplateContext.funcReferencesInRange(file.text, scanStart, scanEnd, index)
                            .forEach { ref ->
                                if (!elementOwnsToken(element, ref.startOffset, ref.endOffset)) return@forEach
                                val fn = index.funcs[ref.funcName] ?: return@forEach
                                references.add(
                                    GoDocPsiReference(
                                        element = element,
                                        absoluteStart = ref.startOffset,
                                        absoluteEnd = ref.endOffset,
                                        target = GoDocTarget(index.rootPath, fn.file, fn.line, fn.column),
                                    ),
                                )
                            }

                        GoDocTemplateContext.templateIncludeReferencesInRange(file.text, scanStart, scanEnd, index)
                            .forEach { ref ->
                                if (!elementOwnsToken(element, ref.startOffset, ref.endOffset)) return@forEach
                                references.add(
                                    GoDocPsiReference(
                                        element = element,
                                        absoluteStart = ref.startOffset,
                                        absoluteEnd = ref.endOffset,
                                        target = GoDocTarget(index.rootPath, ref.targetPath, ref.targetLine, ref.targetColumn),
                                    ),
                                )
                            }

                        val contract = index.contractForFileAt(project, virtualFile.path, scanStart)
                            ?: return@goDocReadAction references.toTypedArray()
                        GoDocTemplateContext.fieldReferencesInRange(file.text, scanStart, scanEnd, index, contract)
                            .forEach { ref ->
                                if (!elementOwnsToken(element, ref.startOffset, ref.endOffset)) return@forEach
                                val owner = index.types[ref.ownerTypeName] ?: return@forEach
                                val target = when {
                                    contract.isTypedRoot(ref.memberName, ref.ownerTypeName) -> GoDocTarget(index.rootPath, owner.file, owner.line, owner.column)
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

                        references.toTypedArray()
                    }
                }
            },
        )
    }
}

private fun elementOwnsToken(element: PsiElement, absoluteStart: Int, absoluteEnd: Int): Boolean {
    val range = element.textRange ?: return false
    if (!range.containsRange(absoluteStart, absoluteEnd)) return false
    val midpoint = absoluteStart + ((absoluteEnd - absoluteStart).coerceAtLeast(1) / 2)
    var leaf = element.containingFile?.findElementAt(midpoint.coerceAtMost((element.containingFile?.textLength ?: 1) - 1))
        ?: return false
    while (leaf.parent != null && leaf.textRange != null && !leaf.textRange.containsRange(absoluteStart, absoluteEnd)) {
        leaf = leaf.parent
    }
    if (leaf == element) return true
    return element.textLength <= maxTemplateReferenceOwnerLength && element.text.contains("{{")
}

private const val maxTemplateReferenceOwnerLength = 500

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
    false,
) {
    override fun resolve(): PsiElement? {
        return goDocReadAction { targetElement(element.project, target) }
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
    val root = target.root ?: goDocReadAction { project.basePath } ?: return null
    val targetFile = targetPath(root, target.file)
    val targetVirtualFile = LocalFileSystem.getInstance()
        .findFileByIoFile(targetFile) ?: return null
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

private fun targetPath(root: String, targetFile: String): File {
    if (targetFile.startsWith("\$GOROOT")) {
        val goRoot = GoDocIndexer.goRoot(File(root)) ?: return File(root, targetFile)
        return File(goRoot, targetFile.removePrefix("\$GOROOT").trimStart('/', '\\'))
    }
    val file = File(targetFile)
    return if (file.isAbsolute) file else File(root, targetFile)
}
