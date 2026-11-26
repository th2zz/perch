package dev.perch.console

import dev.perch.console.core.format.Markdown
import dev.perch.console.core.format.Markdown.Block
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MarkdownTest {

    private fun plain(text: String): String {
        val (out, _) = Markdown.inline(text)
        return out
    }

    private val drawnTable = listOf(
        "  Verified capabilities (each backed by a repeatable check)",
        "   Capability" + " ".repeat(36) + "Verification",
        "  " + "━".repeat(46) + "  " + "━".repeat(60),
        "   Composer guard (unsent prompt check)" + " ".repeat(10) +
            "scripts/verify-composer-guard.sh 6/6",
        "  " + "─".repeat(46) + "  " + "─".repeat(60),
        "   Live refresh (0.8s working / 3s idle)" + " ".repeat(9) + "LiveSessionTest",
        "  " + "─".repeat(46) + "  " + "─".repeat(60),
        "",
        "Then another paragraph.",
    ).joinToString("\n")

    @Test
    fun aTableTheTerminalAlreadyDrewIsReadFromItsBorder() {
        val blocks = Markdown.parse(drawnTable)
        val table = blocks.filterIsInstance<Block.Table>().single()

        assertEquals(listOf("Capability", "Verification"), table.header)
        assertEquals(2, table.rows.size)
        assertEquals("Composer guard (unsent prompt check)", table.rows[0][0])
        assertEquals("scripts/verify-composer-guard.sh 6/6", table.rows[0][1])
        assertEquals("Live refresh (0.8s working / 3s idle)", table.rows[1][0])

        val prose = blocks.filterIsInstance<Block.Paragraph>().joinToString("\n") { it.text }
        assertTrue("the border leaked into the text: $prose", !prose.contains("━"))
        assertEquals(
            "the header line must not survive as its own paragraph",
            0,
            blocks.filterIsInstance<Block.Paragraph>().count { it.text.contains("Capability") },
        )
        assertTrue(blocks.any { it is Block.Paragraph && it.text.contains("Then another paragraph") })
    }

    private val tableFromASession = """
        ### Section 8: Transport Modes

        Comparison of transport options across private networks:

         Mode                                       | Encryption                                 | Setup                                      | Notes
        --------------------------------------------|--------------------------------------------|--------------------------------------------|-------------------------------------------
         Tailscale                                  | WireGuard end-to-end                       | Install app on both devices                | Works across Wi-Fi and cellular
         Local LAN                                  | Local network boundary                     | Same subnet required                       | Lowest latency on home Wi-Fi

        Then another paragraph.
    """.trimIndent()

    @Test
    fun aTableIsReadAsCellsRatherThanAsPipes() {
        val table = Markdown.parse(tableFromASession).filterIsInstance<Block.Table>().single()

        assertEquals(
            listOf("Mode", "Encryption", "Setup", "Notes"),
            table.header,
        )
        assertEquals(2, table.rows.size)
        assertEquals("Tailscale", table.rows[0][0])
        assertEquals("WireGuard end-to-end", table.rows[0][1])
        assertEquals("Install app on both devices", table.rows[0][2])
        assertEquals("Lowest latency on home Wi-Fi", table.rows[1][3])
    }

    @Test
    fun theDashesUnderAHeaderAreSyntaxAndNeverContent() {
        val blocks = Markdown.parse(tableFromASession)

        val prose = blocks.filterIsInstance<Block.Paragraph>().joinToString("\n") { it.text }
        assertTrue("the delimiter row leaked into the text: $prose", !prose.contains("|---"))
        assertTrue("the table should be a table: $blocks", blocks.any { it is Block.Table })
        assertTrue(blocks.any { it is Block.Heading })
        assertTrue(blocks.any { it is Block.Paragraph && it.text.contains("Then another paragraph") })
    }

    @Test
    fun aPipeInProseIsNotATable() {
        // Shell pipelines are why the delimiter row, not the pipe, is the signal.
        assertEquals(
            listOf(Block.Paragraph("cat access.log | grep 404 | wc -l")),
            Markdown.parse("cat access.log | grep 404 | wc -l"),
        )
        assertEquals(
            listOf(
                Block.Paragraph("a | b"),
                // `---` on its own is a rule, and a rule is not a delimiter row: it has
                // no pipe. Two lines must not become a table because of one.
                Block.Rule,
                Block.Paragraph("c | d"),
            ),
            Markdown.parse("a | b\n---\nc | d"),
        )
    }

    @Test
    fun aRowOfCellsWithoutADelimiterStaysALine() {
        assertEquals(
            listOf(Block.Paragraph("Mode | Encryption"), Block.Paragraph("Tailscale | WireGuard")),
            Markdown.parse("Mode | Encryption\nTailscale | WireGuard"),
        )
    }

    @Test
    fun aTableIsReadBorderedOrNot() {
        val table = Markdown
            .parse("| a | b |\n| --- | --- |\n| 1 | 2 |")
            .filterIsInstance<Block.Table>()
            .single()

        assertEquals(listOf("a", "b"), table.header)
        assertEquals(listOf(listOf("1", "2")), table.rows)
    }

    @Test
    fun aRowThatIsMissingCellsDoesNotBreakTheReader() {
        val table = Markdown
            .parse("| a | b | c |\n| --- | --- | --- |\n| 1 | 2 |")
            .filterIsInstance<Block.Table>()
            .single()

        assertEquals(listOf(listOf("1", "2")), table.rows)
    }

    @Test
    fun headingsStopLookingLikeComments() {
        assertEquals(listOf(Block.Heading(2, "What it does")), Markdown.parse("## What it does"))
        assertEquals(listOf(Block.Heading(1, "Top")), Markdown.parse("# Top"))
        // A hash without a space is a hash, not a heading — a shell prompt is full of
        // them.
        assertEquals(listOf(Block.Paragraph("#!/bin/sh")), Markdown.parse("#!/bin/sh"))
    }

    @Test
    fun aLineOfDashesOrBoxDrawingBecomesOneRule() {
        assertEquals(listOf(Block.Rule), Markdown.parse("---"))
        // What agents and their UIs actually draw. Three lines of these on a phone
        // is waste; one divider is the same information.
        assertEquals(listOf(Block.Rule), Markdown.parse("─".repeat(150)))
        assertEquals(listOf(Block.Rule), Markdown.parse("══════════"))
        // Two are not a separator.
        assertEquals(listOf(Block.Paragraph("--")), Markdown.parse("--"))
    }

    @Test
    fun listsKeepTheirStructure() {
        assertEquals(listOf(Block.Bullet("one"), Block.Bullet("two")), Markdown.parse("- one\n- two"))
        assertEquals(
            listOf(Block.Numbered("1.", "Approve"), Block.Numbered("2.", "Reject")),
            Markdown.parse("1. Approve\n2) Reject"),
        )
    }

    @Test
    fun oneLineStaysOneBlockSoNothingReflows() {
        // Markdown says consecutive lines are one paragraph. A terminal has already
        // wrapped them, and joining them would destroy anything laid out on purpose.
        val blocks = Markdown.parse("first  result   12.4\nsecond result    3.1")
        assertEquals(2, blocks.size)
        assertEquals("first  result   12.4", (blocks[0] as Block.Paragraph).text)
    }

    @Test
    fun fencedCodeSurvivesWholeIncludingItsBlankLines() {
        val text = "before\n```python\nx = 1\n\ny = 2\n```\nafter"
        val blocks = Markdown.parse(text)
        assertEquals(3, blocks.size)
        val code = blocks[1] as Block.Code
        assertEquals("python", code.language)
        assertEquals("x = 1\n\ny = 2", code.text)
    }

    @Test
    fun inlineMarkersAreRemovedAndTheirRangesReported() {
        val (plain, spans) = Markdown.inline("say **loudly** and `quietly`")
        assertEquals("say loudly and quietly", plain)
        assertEquals(2, spans.size)
        assertEquals(Markdown.Emphasis.Bold, spans[0].emphasis)
        assertEquals("loudly", plain.substring(spans[0].start, spans[0].end))
        assertEquals(Markdown.Emphasis.Code, spans[1].emphasis)
        assertEquals("quietly", plain.substring(spans[1].start, spans[1].end))
    }

    @Test
    fun strayMarkersInTerminalOutputAreLeftExactlyAsTheyAre() {
        // Every one of these appears in real agent output, and eating the marker
        // would corrupt text the user may need to read character for character.
        for (untouched in listOf("2 * 3 = 6", "ls *.go", "some_var_name", "a * b * c")) {
            assertEquals(untouched, plain(untouched))
        }
        // An unclosed marker is not a marker.
        assertEquals("**still literal", plain("**still literal"))
    }

    @Test
    fun aBoldPhraseFollowedByPunctuationStillCloses() {
        // "**Result:** 12.4" is how agents label a result, and the closing marker is
        // followed by a colon — which has to count as a boundary or nothing renders.
        val (plain, spans) = Markdown.inline("**Result:** 12.4")
        assertEquals("Result: 12.4", plain)
        assertEquals(1, spans.size)
        assertEquals("Result:", plain.substring(spans[0].start, spans[0].end))
    }

    @Test
    fun plainTextKeepsTheWordsAndDropsThePunctuation() {
        // What a notification can carry: an Android notification has no rich text, so
        // the lock screen must show the words rather than `**bold**`. Line by line,
        // because that is how agents write it.
        assertEquals("Running tests", Markdown.plain("**Running** `tests`"))
        assertEquals("first\nsecond", Markdown.plain("**first**\n_second_"))
        // And the same care as the renderer: a marker that means something else stays.
        assertEquals("2 * 3 = 6", Markdown.plain("2 * 3 = 6"))
        assertEquals("some_var_name", Markdown.plain("some_var_name"))
    }

    @Test
    fun aTerminalScreenComesOutAsSomethingReadable() {
        // The shape of a real one, pasted from the trial: an agent's reply with the
        // UI's separators and its input prompt around it.
        val screen = """
            > How should we structure the host bridge and phone client?
            ──────────────────────────────────────
            ## Summary

            Keep the architecture **local-first** for three reasons:

            - No code leaves your network
            - Reconnects survive mobile network switches
            1. Start the bridge daemon
            2. Pair the Android client
        """.trimIndent()

        val blocks = Markdown.parse(screen)
        assertEquals(Block.Quote("How should we structure the host bridge and phone client?"), blocks[0])
        assertEquals(Block.Rule, blocks[1])
        assertEquals(Block.Heading(2, "Summary"), blocks[2])
        assertTrue(
            "the emphasis must be a *span*, not characters the user has to read",
            Markdown.inline("Keep the architecture **local-first** for three reasons:").second.isNotEmpty(),
        )
        assertEquals(
            "Keep the architecture local-first for three reasons:",
            plain("Keep the architecture **local-first** for three reasons:"),
        )
        // By content rather than by index: the point is that structure survived,
        // not where in the list it landed.
        assertTrue("bullets survive: $blocks", Block.Bullet("No code leaves your network") in blocks)
        assertTrue("bullets survive: $blocks", Block.Bullet("Reconnects survive mobile network switches") in blocks)
        assertTrue("numbered items survive: $blocks", Block.Numbered("1.", "Start the bridge daemon") in blocks)
        assertTrue("numbered items survive: $blocks", Block.Numbered("2.", "Pair the Android client") in blocks)
    }
}
