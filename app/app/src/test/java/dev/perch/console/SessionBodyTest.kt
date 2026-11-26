package dev.perch.console

import dev.perch.console.core.format.SessionBody
import org.junit.Assert.assertEquals
import org.junit.Test

class SessionBodyTest {

    private val hisScreen = """
        • Let me do that first. (42s • esc to interrupt)
        └ Tip: Press tab to queue a message when a task is running.


        › Ask Codex to do anything

        DeepSeek-Flash high · Context 41% used · 6.8M used
        ← for agents · ? for shortcuts
    """.trimIndent()

    @Test
    fun theFurnitureGoesAndTheAnswerStays() {
        val body = SessionBody.of(hisScreen, chromeLines = 4)

        assertEquals(
            "• Let me do that first. (42s • esc to interrupt)\n" +
                "└ Tip: Press tab to queue a message when a task is running.",
            body,
        )
    }

    @Test
    fun nothingIsDroppedWhenTheComputerCouldNotTell() {
        // Zero is what an agent nobody has measured reports, and it has to mean "leave the
        // screen alone" — the alternative is guessing at somebody else's layout.
        assertEquals(hisScreen, SessionBody.of(hisScreen, chromeLines = 0))
        assertEquals(hisScreen, SessionBody.of(hisScreen, chromeLines = -1))
    }

    @Test
    fun aCountLargerThanTheTextDoesNotCrashTheReader() {
        assertEquals("", SessionBody.of("one line", chromeLines = 99))
        assertEquals("", SessionBody.of("", chromeLines = 4))
    }

    @Test
    fun trailingBlankLinesGoWithTheBoxTheyWereMadeFor() {
        val text = "the answer\n\n\n› box"

        assertEquals("the answer", SessionBody.of(text, chromeLines = 3))
    }

    /**
     * The screen read usually ends with a newline, and that is not a line.
     *
     * This case came out of the rendered screenshot rather than out of the tests: the
     * fixture above happens not to end with one, so counting the empty string after the
     * last row shifted everything by a line and left the input box sitting in the reading
     * view. Looking at the picture is what found it.
     */
    @Test
    fun aTrailingNewlineIsNotAnExtraLine() {
        val withTrailing = "the answer\n\n› box\nDeepSeek-Flash high · Context 41% used\n"

        assertEquals("the answer", SessionBody.of(withTrailing, chromeLines = 3))
    }
}
