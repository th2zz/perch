package dev.perch.console.core.format

object SessionBody {

    fun of(text: String, chromeLines: Int): String {
        if (chromeLines <= 0) return text
        val lines = text.split('\n')
        // A screen read usually ends with a newline, which split() turns into one empty
        // string after the last row. Counting that as a line put the input box back in the
        // reading view — caught by the rendered screenshot of this very screen, not by the
        // text test, because the text test's fixture happened not to end with a newline.
        val rows = if (lines.isNotEmpty() && lines.last().isEmpty()) lines.dropLast(1) else lines
        val kept = rows.dropLast(chromeLines.coerceAtMost(rows.size)).toMutableList()
        // The blank line that separated the answer from the box is furniture by another
        // name: it existed to make room for the box, and it reads as a gap after nothing.
        while (kept.isNotEmpty() && kept.last().isBlank()) {
            kept.removeAt(kept.size - 1)
        }
        return kept.joinToString("\n")
    }
}
