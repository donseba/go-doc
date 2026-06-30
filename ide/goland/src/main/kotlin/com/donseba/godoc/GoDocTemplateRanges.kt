package com.donseba.godoc

import com.intellij.psi.PsiElement

private const val maxTemplateActionScanLength = 500

internal fun goDocTemplateActionRange(element: PsiElement, text: String): Pair<Int, Int>? {
    val range = element.textRange ?: return null
    val start = range.startOffset.coerceIn(0, text.length)
    val end = range.endOffset.coerceIn(start, text.length)
    val open = text.lastIndexOf("{{", start)
    if (open < 0) return null
    val close = text.indexOf("}}", start)
    if (close < 0 || close + 2 < end) return null
    if (close + 2 - open > maxTemplateActionScanLength) return null
    return open to close + 2
}
