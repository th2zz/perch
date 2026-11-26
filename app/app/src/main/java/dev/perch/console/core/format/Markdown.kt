package dev.perch.console.core.format

object Markdown {

    /** One rendered block. */
    sealed interface Block {
        data class Heading(val level: Int, val text: String) : Block
        data class Paragraph(val text: String) : Block
        data class Bullet(val text: String) : Block
        data class Numbered(val marker: String, val text: String) : Block
        data class Quote(val text: String) : Block
        data class Code(val language: String, val text: String) : Block

        /**
         * A table, with the cells as written and the alignment thrown away.
         *
         * The alignment is the terminal's, not the document's: these arrive padded
         * with spaces to a common column width, which is exactly what a phone cannot
         * show. [Markdown.inline] still applies inside each cell, so a cell that is
         * **bold** stays bold.
         */
        data class Table(val header: List<String>, val rows: List<List<String>>) : Block

        data object Rule : Block
    }

    /** The three inline styles this reader understands. */
    enum class Emphasis { Bold, Italic, Code }

    /** A styled run: `[start, end)` into the marker-free text. */
    data class Span(val start: Int, val end: Int, val emphasis: Emphasis)

    private val heading = Regex("^(#{1,6})\\s+(.*)$")
    private val bullet = Regex("^[-*]\\s+(.*)$")
    private val numbered = Regex("^(\\d{1,3})[.)]\\s+(.*)$")
    private val quote = Regex("^>\\s?(.*)$")

    /**
     * A row of cells and the line that says so, nothing else in a terminal's output
     * looks like this: dashes and pipes joined into at least two columns.
     *
     * `----` on its own is a horizontal rule (and a terminal draws rules with `─`),
     * so a pipe is required — which is also what keeps a shell pipeline or an
     * aligned ASCII box from being mistaken for a table.
     */
    private val delimiterRow = Regex("^\\|?\\s*:?-+:?\\s*(\\|\\s*:?-+:?\\s*)*\\|?$")

    fun parse(text: String): List<Block> {
        val out = mutableListOf<Block>()
        val lines = text.replace("\r\n", "\n").split('\n')
        var i = 0
        while (i < lines.size) {
            val line = lines[i].trimEnd()
            val trimmed = line.trim()
            if (trimmed.startsWith("```")) {
                val language = trimmed.removePrefix("```").trim()
                val body = StringBuilder()
                i++
                while (i < lines.size && !lines[i].trim().startsWith("```")) {
                    body.append(lines[i].trimEnd()).append('\n')
                    i++
                }
                if (i < lines.size) i++ // the closing fence
                out += Block.Code(language, body.toString().trimEnd('\n'))
                continue
            }
            // A table, before anything else looks at the line: it is the one block
            // that spans lines, and both of its first two lines contain characters
            // that other rules would claim (`|` is not special, but a row of dashes
            // *is* a rule, and the header would otherwise be a paragraph of pipes).
            val table = tableAt(lines, i)
            if (table != null) {
                out += table.block
                i = table.next
                continue
            }
            val drawn = drawnTableAt(lines, i, out)
            if (drawn != null) {
                if (drawn.replaced != null) out.removeAt(out.size - 1)
                out += drawn.block
                i = drawn.next
                continue
            }
            val block: Block? = when {
                trimmed.isEmpty() -> null
                isRule(trimmed) -> Block.Rule
                heading.matches(trimmed) -> {
                    val m = heading.find(trimmed)!!
                    Block.Heading(m.groupValues[1].length, m.groupValues[2])
                }
                bullet.matches(trimmed) -> Block.Bullet(bullet.find(trimmed)!!.groupValues[1])
                numbered.matches(trimmed) -> {
                    val m = numbered.find(trimmed)!!
                    Block.Numbered(m.groupValues[1] + ".", m.groupValues[2])
                }
                quote.matches(trimmed) -> Block.Quote(quote.find(trimmed)!!.groupValues[1])
                else -> Block.Paragraph(line)
            }
            if (block != null) out += block
            i++
        }
        return out
    }

    /**
     * Reads a table starting at [start], or returns null when there is not one.
     *
     * The signal is the *second* line, never the first: a row of cells under a row of
     * dashes-and-pipes is a table header, and a row of cells on its own is a sentence
     * that happens to contain a pipe. That is the whole of the rule — and it is why
     * this reader can afford to have tables at all, because the alternative (trust a
     * pipe) would misfire on every `grep -v x | wc -l` a terminal prints.
     *
     * This is the one block allowed to span lines, and the exception is worth stating
     * because the opposite is a rule of this reader: one terminal line stays one block,
     * since reflowing hard-wrapped output destroys deliberate layout. A table's rows
     * are not a paragraph being wrapped; they are one object whose lines are cells of
     * the same record, which is exactly why it has to be read as a unit to be shown as
     * anything but pipes.
     */
    private fun tableAt(lines: List<String>, start: Int): TableRead? {
        if (start + 1 >= lines.size) return null
        val headerLine = lines[start]
        val delimiter = lines[start + 1].trim()
        if (!headerLine.contains('|') || !delimiter.contains('|')) return null
        if (!delimiter.matches(delimiterRow)) return null
        // `cells`, not `drawnCells`: this reader splits on pipes. A string-level edit once swapped
        // this line for the drawn reader's, which left a pipe table's header gap-split and its rows
        // pipe-split — one table, two rules, and a test that read as nonsense until it was printed.
        val header = cells(headerLine)
        if (header.size < 2) return null

        val rows = mutableListOf<List<String>>()
        var i = start + 2
        while (i < lines.size && lines[i].contains('|')) {
            val row = cells(lines[i])
            if (row.isEmpty()) break
            rows += row
            i++
        }
        return TableRead(Block.Table(header, rows), i)
    }

    private data class TableRead(val block: Block.Table, val next: Int)

    private fun drawnTableAt(lines: List<String>, start: Int, emitted: List<Block>): DrawnRead? {
        if (columnCount(lines[start]) < 2) return null
        if (start == 0) return null
        val headerLine = lines[start - 1]
        val previous = emitted.lastOrNull()
        if (previous !is Block.Paragraph || previous.text.trim() != headerLine.trim()) return null

        val header = drawnCells(headerLine)
        if (header.size < 2) return null

        val rows = mutableListOf<List<String>>()
        var i = start + 1
        while (i < lines.size) {
            if (columnCount(lines[i]) >= 2) {
                i++
                continue
            }
            if (lines[i].isBlank()) break
            rows += drawnCells(lines[i])
            i++
        }
        if (rows.isEmpty()) return null
        return DrawnRead(Block.Table(header, rows), i, previous)
    }

    private data class DrawnRead(val block: Block.Table, val next: Int, val replaced: Block?)

    /** How many columns a border line has: its runs of drawing characters. */
    private fun columnCount(border: String): Int {
        var count = 0
        var index = 0
        while (index < border.length) {
            if (border[index] in boxCharacters) {
                val from = index
                while (index < border.length && border[index] in boxCharacters) index++
                if (index - from >= 3) count++
            } else {
                index++
            }
        }
        return count
    }

    /**
     * A row's cells from a table the terminal drew, split where it left a gap.
     *
     * Named differently from the pipe form's [cells] on purpose: a `|`-delimited row is split on its
     * pipes, while a drawn row is split on the gap the terminal aligned (two spaces or more — one
     * space is ordinary text inside a cell, e.g. "0.8s working / 3s idle").
     */
    private fun drawnCells(row: String): List<String> =
        row.trim().split(Regex("\\s{2,}")).map { it.trim() }.filter { it.isNotEmpty() }

    private val boxCharacters = setOf('─', '━', '═', '╌', '╍', '┄', '┅', '┈', '┉')

    private fun cells(line: String): List<String> {
        val body = line.trim().removePrefix("|").removeSuffix("|")
        return body.split('|').map { it.trim() }
    }

    /**
     * The same text with the markers removed and no styling.
     *
     * For the place that cannot render: an Android notification shows plain text, so
     * what reaches the lock screen has to be the *words*, not `**bold**`. Line by
     * line, because a marker never spans a line in the output agents produce, and a
     * lock screen showing raw punctuation is exactly the complaint that started this.
     */
    fun plain(text: String): String =
        text.lines().joinToString("\n") { line -> inline(line).first }

    /** A line that is nothing but a separator, in either alphabet. */
    private fun isRule(trimmed: String): Boolean {
        if (trimmed.length < 3) return false
        if (trimmed.all { it == '-' } || trimmed.all { it == '*' } || trimmed.all { it == '_' }) return true
        return trimmed.all { it == '─' } || trimmed.all { it == '═' }
    }

    /**
     * Splits markers from text, and reports where the styling goes.
     *
     * Returns the text *without* markers plus spans into it. A marker that never
     * closes is left alone rather than swallowed: terminal output is full of stray
     * asterisks and underscores (`2 * 3`, a glob, a variable name), and eating them
     * would corrupt text the user may need to read exactly as it is.
     */
    fun inline(text: String): Pair<String, List<Span>> {
        val out = StringBuilder()
        val spans = mutableListOf<Span>()
        var i = 0
        while (i < text.length) {
            val emphasis = when {
                text.startsWith("**", i) -> Emphasis.Bold
                text.startsWith("__", i) -> Emphasis.Bold
                text[i] == '*' -> Emphasis.Italic
                text[i] == '_' -> Emphasis.Italic
                text[i] == '`' -> Emphasis.Code
                else -> null
            }
            if (emphasis == null) {
                out.append(text[i])
                i++
                continue
            }
            val marker = when (emphasis) {
                Emphasis.Code -> "`"
                Emphasis.Bold -> if (text.startsWith("**", i)) "**" else "__"
                Emphasis.Italic -> text.substring(i, i + 1)
            }
            if (!canOpen(text, i, marker, emphasis)) {
                out.append(text[i])
                i++
                continue
            }
            val close = text.indexOf(marker, i + marker.length)
            if (close < 0 || close == i + marker.length || !canClose(text, close, marker, emphasis)) {
                out.append(text[i])
                i++
                continue
            }
            val start = out.length
            val (plain, nested) = inline(text.substring(i + marker.length, close))
            out.append(plain)
            spans += Span(start, out.length, emphasis)
            for (span in nested) {
                spans += Span(start + span.start, start + span.end, span.emphasis)
            }
            i = close + marker.length
        }
        return out.toString() to spans
    }

    private fun canOpen(text: String, i: Int, marker: String, emphasis: Emphasis): Boolean {
        if (emphasis == Emphasis.Code) return true
        val after = i + marker.length
        if (after >= text.length || text[after].isWhitespace()) return false
        if (i == 0) return true
        return !isAsciiWordChar(text[i - 1])
    }

    /**
     * Whether a marker can *end* emphasis.
     *
     * Nothing with a space before it (that is prose around an asterisk, not a
     * closing marker), and nothing followed by a letter or digit (that is a word
     * continuing). Punctuation has to be allowed to close: agents label results as
     * `**Result:** 12.4`, and the marker is immediately followed by punctuation,
     * which is why the check is "not an ASCII letter or digit".
     */
    private fun canClose(text: String, close: Int, marker: String, emphasis: Emphasis): Boolean {
        if (emphasis == Emphasis.Code) return true
        if (close == 0 || text[close - 1].isWhitespace()) return false
        val after = close + marker.length
        if (after >= text.length) return true
        return !isAsciiWordChar(text[after])
    }
}

private fun isAsciiWordChar(ch: Char): Boolean =
    ch in 'a'..'z' || ch in 'A'..'Z' || ch in '0'..'9'
