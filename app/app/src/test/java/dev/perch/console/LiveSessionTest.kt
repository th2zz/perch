package dev.perch.console

import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.ConversationMessage
import dev.perch.console.core.herdrclient.Transcript
import dev.perch.console.features.terminal.LiveSession
import org.junit.Assert.assertFalse
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class LiveSessionTest {

    private val open = SessionView(
        hostId = "local",
        hostLabel = "this computer",
        paneId = "wD:p1",
        workspaceLabel = "perch",
        agent = "agy",
        agentStatus = "idle",
        title = "perch",
        text = "> Write the architecture summary",
        revision = 4,
    )

    /**
     * A read that says the same thing is not a change.
     *
     * This is the difference between a live screen and a screen that flickers: every
     * poll carries a fresh read time, and herdr's revision moves for reasons that
     * have nothing to do with what is on screen (focus, a resize). Comparing whole
     * objects would repaint — and, before the follow rule, re-anchor the scroll —
     * forty times a minute on a session where nothing happened at all.
     */
    @Test
    fun aSecondReadOfTheSameScreenIsNotAChange() {
        val again = open.copy(revision = 9, lines = 400, source = "visible")

        assertFalse(
            "the same text under the same status is the same screen",
            LiveSession.differs(open, again),
        )
    }

    /** The agent answering, or asking, is a change — that is the whole point. */
    @Test
    fun newTextIsAChange() {
        assertTrue(LiveSession.differs(open, open.copy(text = "> Write the architecture summary\nok")))
    }

    @Test
    fun aPollAsksForEverythingTheScreenIsHolding() {
        val shown = (1..688).joinToString("\n") { "line $it" }

        assertEquals(688, LiveSession.pollDepth(shown))
        assertTrue(
            "a bigger screen must never poll for less than a smaller one did",
            LiveSession.pollDepth((1..400).joinToString("\n") { "$it" }) <= LiveSession.pollDepth(shown),
        )
    }

    /** A screen with almost nothing on it still polls with the floor, so a quiet session still notices. */
    @Test
    fun aShortScreenStillPollsWithTheFloor() {
        assertEquals(LiveSession.MIN_POLL_LINES, LiveSession.pollDepth("> hi"))
        assertEquals(
            "an empty screen is not an instruction to read nothing",
            LiveSession.MIN_POLL_LINES,
            LiveSession.pollDepth(""),
        )
    }

    /** So is the state light, which the user reads at a glance. */
    @Test
    fun aNewStateIsAChange() {
        assertTrue(LiveSession.differs(open, open.copy(agentStatus = "blocked")))
    }

    @Test
    fun textWaitingInTheTerminalIsAChange() {
        assertTrue(LiveSession.differs(open, open.copy(unsent = "partial draft text")))
    }

    /**
     * A dialog appearing while the screen is open is a change, and it has to be:
     * the keys and the Approve button appear with it, and they are the reason the
     * user is looking.
     */
    @Test
    fun aDialogAppearingIsAChange() {
        val blocked = open.copy(
            agentStatus = "blocked",
            rule = "claude/bash_permission_prompt",
            decisionId = "local/wD:p1@1",
            approvable = true,
            rejectable = true,
        )

        assertTrue(LiveSession.differs(open, blocked))
    }

    @Test
    fun followingIsAboutWhereTheUserLeftTheView() {
        assertTrue("at the end", LiveSession.following(offset = 900, max = 900))
        assertTrue(
            "a fling that stops on the end, rounded",
            LiveSession.following(offset = 896, max = 900),
        )
        assertFalse("scrolled up to read", LiveSession.following(offset = 200, max = 900))
        assertFalse(
            "two lines up is reading, not following — this is the case that cost the " +
                "user's place mid-answer",
            LiveSession.following(offset = 860, max = 900),
        )
    }

    /** Text shorter than the screen has no scroll range, and that is "at the end". */
    @Test
    fun textThatFitsIsAlwaysAtTheEnd() {
        assertTrue(LiveSession.following(offset = 0, max = 0))
    }

    @Test
    fun aReadWhileReadingKeepsTheWordsAndMovesEverythingElse() {
        val shown = open.copy(text = "Chapter 1: intro and overview")
        val fresh = open.copy(
            text = "Chapter 3: details and new line",
            agentStatus = "blocked",
            approvable = true,
            truncated = true,
        )

        val merged = LiveSession.afterRead(shown, fresh, following = false)

        assertEquals("the words stay where the reader left them", shown.text, merged.text)
        assertEquals("so does the notice about missing older output", shown.truncated, merged.truncated)
        assertEquals("but the light moves", "blocked", merged.agentStatus)
        assertTrue("and so does the button the user will need next", merged.approvable)
    }

    /** A reader who is at the end asked for the end, so they get the newest text. */
    @Test
    fun aReadWhileFollowingTakesTheNewestText() {
        val shown = open.copy(text = "old")
        val fresh = open.copy(text = "old\nnew")

        assertEquals("old\nnew", LiveSession.afterRead(shown, fresh, following = true).text)
    }

    @Test
    fun onlyAWorkingAgentEarnsTheFastRefresh() {
        assertEquals(LiveSession.POLL_MILLIS_WORKING, LiveSession.pollInterval("working"))
        for (still in listOf("idle", "done", "blocked", "unknown", "")) {
            assertEquals(
                "$still is not being written to, so it does not need the fast interval",
                LiveSession.POLL_MILLIS_IDLE,
                LiveSession.pollInterval(still),
            )
        }
        assertTrue(LiveSession.POLL_MILLIS_WORKING < LiveSession.POLL_MILLIS_IDLE)
    }

    // ------------------------------------------------------------ the conversation's own rules

    private val said = Transcript(
        readable = true,
        messages = listOf(ConversationMessage("you", "Fix this"), ConversationMessage("agent", "Done.")),
    )

    /**
     * A conversation that did not change must not repaint.
     *
     * This poll runs every 0.8 seconds while an agent works, and the reading area is Markdown that
     * has to be parsed and laid out again on every recomposition. A comparison that counted the
     * seconds under "working" as noise would be right — but it would also freeze the one number that
     * is supposed to move, so the rule is: everything that is drawn, and nothing else.
     */
    @Test
    fun aSecondReadOfTheSameConversationIsNotAChange() {
        assertFalse(LiveSession.conversationDiffers(said, said.copy()))
        assertTrue(LiveSession.conversationDiffers(said, said.copy(working = true)))
        assertTrue(LiveSession.conversationDiffers(said, said.copy(workingSecs = 42)))
        assertTrue(LiveSession.conversationDiffers(said, said.copy(truncated = true)))
        assertTrue(
            "a new answer is a change",
            LiveSession.conversationDiffers(
                said,
                said.copy(messages = said.messages + ConversationMessage("agent", "One more update.")),
            ),
        )
        assertTrue(
            "the computer losing the record is a change: it sends the screen back to the terminal's text",
            LiveSession.conversationDiffers(said, said.copy(readable = false)),
        )
    }

    /**
     * Whether to say "working", against what each source actually knows.
     *
     * The agent's own record is asked first, because herdr's status is about the *pane*: it stays
     * "working" through the pause after an agent finishes a turn and before the next line of output
     * arrives, and it cannot tell a paused agent from a thinking one. When there is no record to ask
     * — an agent nobody has taught Perch to read — herdr is the only thing there is.
     */
    @Test
    fun theRecordDecidesWhetherTheAgentIsWorkingWhenThereIsOne() {
        assertTrue(LiveSession.isWorking(said.copy(working = true), agentStatus = "idle"))
        assertFalse(LiveSession.isWorking(said.copy(working = false), agentStatus = "working"))

        val unreadable = Transcript(readable = false)
        assertTrue(LiveSession.isWorking(unreadable, agentStatus = "working"))
        assertFalse(LiveSession.isWorking(unreadable, agentStatus = "idle"))
        assertFalse(LiveSession.isWorking(null, agentStatus = "blocked"))
    }

    /**
     * No record, no duration.
     *
     * The number under "working · 2 minutes" is the computer's own measurement of the turn, and the
     * phone has no business computing it from two clocks that can disagree. Zero means "nobody
     * measured it", and the label then says "working" with no time rather than a stopwatch this app
     * invented.
     */
    @Test
    fun aDurationIsOnlyShownWhenSomebodyMeasuredOne() {
        assertEquals(150, LiveSession.workingSeconds(said.copy(working = true, workingSecs = 150)))
        assertEquals(0, LiveSession.workingSeconds(Transcript(readable = false, workingSecs = 150)))
        assertEquals(0, LiveSession.workingSeconds(null))
    }
}
