package dev.perch.console

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.v2.createComposeRule
import dev.perch.console.core.format.Markdown
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class SmoothnessBudgetTest {

    @get:Rule
    val compose = createComposeRule()

    /** A long answer of the shape an agent actually produces: prose, lists, and a table. */
    private fun transcript(extraLines: Int): String = buildString {
        for (section in 1..8) {
            append("### Section $section: Parser Notes\n\n")
            append("This section explains block parsing and inline formatting rules for phone screens.\n\n")
            for (item in 1..8) {
                append("- **Point $item**: keep headings, lists, and code blocks clearly separated.\n")
            }
            append("\n Mode | Status | Latency\n")
            append("------|--------|--------\n")
            append(" LAN  | ready  | 4ms\n")
            append(" VPN  | ready  | 18ms\n\n")
        }
        for (line in 1..extraLines) {
            append("Additional detail line $line: representative long paragraph text for layout budget checks.\n")
        }
    }

    @Test
    fun readingALongAnswerIsCheaperThanAFrame() {
        val text = transcript(200)
        repeat(3) { Markdown.parse(text) } // warm the JIT up

        val started = System.nanoTime()
        val rounds = 10
        repeat(rounds) { Markdown.parse(text) }
        val perParse = (System.nanoTime() - started) / rounds / 1_000_000

        println("Markdown.parse: ${perParse}ms for ${text.lines().size} lines")
        assertTrue(
            "parsing one answer must not cost more than a frame ($perParse ms)",
            perParse < 40,
        )
    }

    /**
     * A refresh, end to end: new text in, screen updated.
     *
     * This is the number the user feels — one poll's worth of work, including the
     * composition and layout of the whole transcript, measured the way the app does it
     * (a state change on the conversation screen). While the agent streams, this happens
     * once per poll, so it is also the frame rate of the stream.
     */
    @Test
    fun oneRefreshDoesNotCostASlideshow() {
        val base = transcript(150)
        refreshBudget(base)
    }

    @Test
    fun aFullScrollbackRefreshIsStillNotASlideshow() {
        refreshBudget(transcript(600))
    }

    private fun refreshBudget(base: String) {
        var session by mutableStateOf(sessionView(base))
        compose.setContent { PerchTheme { SessionScreen(session = session, busy = false, onApprove = {}, onReject = {}, onSend = {}, onRefresh = {}, onClose = {}) } }
        compose.waitForIdle()

        println("refresh budget: ${base.lines().size} lines on screen")
        val times = mutableListOf<Long>()
        repeat(6) { round ->
            val next = base + "\nNew line $round: agent continues streaming.\n"
            val started = System.nanoTime()
            session = sessionView(next)
            compose.waitForIdle()
            times += (System.nanoTime() - started) / 1_000_000
        }

        val median = times.sorted()[times.size / 2]
        println("one refresh: ${times.sorted()} ms (median ${median}ms)")
        assertTrue(
            "a refresh must stay well under a human-noticeable hitch (median ${median}ms)",
            median < 120,
        )
    }

    private fun sessionView(text: String) = SessionView(
        paneId = "wD:p1",
        workspaceLabel = "perch",
        agent = "agy",
        displayAgent = "agy",
        agentStatus = "working",
        text = text,
    )
}
