package dev.perch.console.core.format

/**
 * Trimming a decision's excerpt down to something a notification can carry.
 *
 * Kept separate from the notification code so the rule is testable without an
 * Android runtime, and so the in-app card and the notification excerpt cannot
 * drift into two different notions of "the last few lines".
 *
 * Two rules, because the two surfaces want different things and getting this
 * wrong is invisible until you look at a rendered card:
 *
 *  1. The *window* is the tail. A banner from twenty minutes ago is noise, so the
 *     last few lines are what is worth reading.
 *  2. The *headline* is the first line of that window, not the last. A dialog puts
 *     its question above its options, so the last line is very often "3. No" —
 *     which is the least useful thing on the screen and was, briefly, what the
 *     collapsed card showed. A log is the opposite, but a log is not what needs a
 *     decision.
 *
 * Blank lines are dropped either way: they cost a line and say nothing.
 */
object NotificationText {

    /// Enough to hold a question and the three options under it.
    const val DEFAULT_LINES = 8

    fun excerpt(text: String, maxLines: Int = DEFAULT_LINES): String =
        text.lines()
            .map { it.trimEnd() }
            .filter { it.isNotBlank() }
            .takeLast(maxLines)
            .joinToString("\n")

    /**
     * The single line a collapsed card or notification shows: what is being asked.
     *
     * Finding it takes a rule rather than an index. Taking the first line of the
     * window runs into whatever noise happens to sit at the top of those eight
     * lines — a spinner caption, a heading from the agent's own output — and
     * taking the last line gives an option ("3. No"). What holds for every dialog
     * in the manifests is the layout: the question sits immediately above the
     * options. So: find the first option, then take the line above it.
     *
     * The option pattern is deliberately narrow. It matches the shapes the agents
     * actually draw — a numbered option with or without the cursor marker, or a
     * bracketed yes/no — and nothing that could be prose. A file path like
     * "2.0.1" will not match; a sentence starting with "No" will not match.
     */
    fun headline(text: String): String {
        val lines = excerpt(text).lines().map { it.trim() }.filter { it.isNotEmpty() }
        if (lines.isEmpty()) return ""

        val firstOption = lines.indexOfFirst { OPTION_LINE.containsMatchIn(it) }
        if (firstOption > 0) return lines[firstOption - 1]
        // No options on screen, so this is not a menu. The opening line of the
        // window is the best available statement of what is going on.
        return lines.first()
    }

    private val OPTION_LINE = Regex(
        """^\s*[❯>›]?\s*(?:\d+[.)]\s+\S|[\[(]\s*[yYnN]\s*[/|]\s*[nNyY]\s*[\])])""",
    )

    /** Kept for callers that genuinely want the newest line of output. */
    fun summary(text: String): String {
        val excerpt = excerpt(text)
        if (excerpt.isEmpty()) return ""
        // The last non-blank line is where the question is, and one line is all a
        // collapsed notification gets.
        return excerpt.lines().last().trim()
    }
}
