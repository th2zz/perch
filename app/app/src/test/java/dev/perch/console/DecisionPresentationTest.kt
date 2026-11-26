package dev.perch.console

import dev.perch.console.core.format.NotificationText
import dev.perch.console.core.format.Time
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionKind
import java.time.Instant
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * What the user sees on a card and in a notification.
 *
 * These are the decisions that determine whether one tap is enough, so they are
 * worth pinning: which buttons may be offered, and how much of a terminal excerpt
 * is shown.
 */
class DecisionPresentationTest {

    @Test
    fun onlyActionsTheComputerCanCarryOutAreOffered() {
        val both = Decision(approvable = true, rejectable = true)
        assertEquals(listOf(DecisionKind.APPROVE, DecisionKind.REJECT), both.availableActions)

        val approveOnly = Decision(approvable = true, rejectable = false)
        assertEquals(listOf(DecisionKind.APPROVE), approveOnly.availableActions)

        // A dialog we can confirm but not cancel, e.g. agy's trust prompt.
        val rejectOnly = Decision(approvable = false, rejectable = true)
        assertEquals(listOf(DecisionKind.REJECT), rejectOnly.availableActions)
    }

    @Test
    fun anUnmappablePromptSaysSoInsteadOfOfferingDeadButtons() {
        val unmappable = Decision(
            approvable = false,
            rejectable = false,
            unmappableReason = "herdr reported a blocked agent but did not name a detection rule",
        )
        assertTrue(unmappable.availableActions.isEmpty())
        assertTrue(unmappable.needsComputer)

        assertFalse(Decision(approvable = true).needsComputer)
    }

    @Test
    fun aDialogThatTakesATypedAnswerIsNotSentToTheComputer() {
        // Answering is a real capability with its own evidence, so a dialog that
        // has no deterministic keys but does accept text must not be written off
        // — that would cost the user a trip for nothing.
        val typed = Decision(approvable = false, rejectable = false, answerable = true)
        assertFalse(typed.needsComputer)
        assertTrue(typed.availableActions.isEmpty())

        // And a menu that takes neither is genuinely a job for the computer.
        val nothing = Decision(approvable = false, rejectable = false, answerable = false)
        assertTrue(nothing.needsComputer)
    }

    @Test
    fun aDialogWeCanAnswerIsNotDescribedAsOneWeCannotHandle() {
        // The rendered card said "Perch cannot press this one for you" directly above
        // an Answer button, because the guidance was gated on keys rather than on
        // whether anything at all could be done.
        val answerOnly = Decision(approvable = false, rejectable = false, answerable = true)
        assertTrue("the card must not claim it is helpless", !answerOnly.needsComputer)

        val helpless = Decision(approvable = false, rejectable = false, answerable = false)
        assertTrue(helpless.needsComputer)

        // And a dialog we can press is never described that way either.
        assertFalse(Decision(approvable = true).needsComputer)
        assertFalse(Decision(rejectable = true).needsComputer)
    }

    @Test
    fun excerptKeepsTheEndOfTheOutputBecauseThatIsWhereTheQuestionIs() {
        val output = """
            ⠹ Working on the migration…

            Running tests
            ✓ 41 passed
            ✗ 2 failed

            Do you want to proceed?
            ❯ 1. Yes
              2. No
        """.trimIndent()

        val excerpt = NotificationText.excerpt(output)
        val lines = excerpt.lines()

        assertEquals(7, lines.size)
        assertTrue(excerpt.endsWith("2. No"))
        assertFalse("blank lines cost a notification line", excerpt.contains("\n\n"))
        // The window is eight lines, so a spinner caption can be inside the
        // expanded text — that is what "expanded" is for. What matters is that it
        // never becomes the one line the collapsed view shows; the headline test
        // below holds that.
        assertFalse(
            "the spinner is never the summary",
            NotificationText.headline(output) == "⠹ Working on the migration…",
        )
    }

    @Test
    fun theCollapsedLineIsTheQuestionNotTheLastOption() {
        // A rendered card showed "3. No" as the whole summary, with the question
        // cut off above it. The window is the tail; the headline is its first line.
        val dialog = """
            ⠹ Working

            Bash command
              npm run test:integration
            Do you want to proceed?
            ❯ 1. Yes
              2. Yes, and don't ask again
              3. No
        """.trimIndent()

        // The spinner is inside the window and is not the answer to "what is it
        // asking": the question is the line above the first option.
        assertEquals("Do you want to proceed?", NotificationText.headline(dialog))
        assertTrue(
            "the options must survive in the expanded form",
            NotificationText.excerpt(dialog).contains("3. No"),
        )
    }

    @Test
    fun aLongDialogKeepsItsQuestionInsideTheWindow() {
        // Three options plus a command line is already more than the old
        // four-line window held, which is exactly how the question fell off.
        val dialog = (1..3).joinToString("\n") { "$it. Option $it" }
        val text = "The question\n$dialog"
        assertEquals("The question", NotificationText.headline(text))
    }

    @Test
    fun aBracketedYesNoGetsItsQuestionToo() {
        // codex's weak_blocker is a "[y/n]" prompt, a different shape from a
        // numbered menu. The same rule has to find its question.
        val text = "Overwrite the generated client?\n[y/n]"
        assertEquals("Overwrite the generated client?", NotificationText.headline(text))
    }

    @Test
    fun proseThatMerelyLooksLikeAnOptionIsNotOne() {
        // "2.0.1" is a version, not an option, and the line above it is not a
        // question. Falling back to the window's opening line is the honest
        // outcome; inventing one would be worse.
        val text = "bumped to\n2.0.1 and published"
        assertEquals("bumped to", NotificationText.headline(text))
    }

    @Test
    fun summaryIsTheLastLineOnly() {
        val output = "Building\nDo you want to proceed?"
        assertEquals("Do you want to proceed?", NotificationText.summary(output))
    }

    @Test
    fun excerptSurvivesUnicodeAndMixedScripts() {
        val output = "Compiling…\nContinue with deployment (y/n)?\n❯ 1. Yes — café\n  2. No"
        assertEquals(output, NotificationText.excerpt(output))
    }

    @Test
    fun emptyExcerptIsEmptyRatherThanAStrayNewline() {
        assertEquals("", NotificationText.excerpt("\n\n   \n"))
        assertEquals("", NotificationText.summary(""))
    }

    @Test
    fun agoSaysSomethingAPersonWouldSay() {
        val now = Instant.parse("2026-10-13T08:00:00Z")
        // The bridge writes RFC 3339 with nanoseconds, as Go does.
        assertEquals("just now", Time.ago("2026-10-13T07:59:40.123456789+00:00", now))
        assertEquals("4 min ago", Time.ago("2026-10-13T07:56:00+00:00", now))
        assertEquals("2 h ago", Time.ago("2026-10-13T06:00:00+00:00", now))
        assertEquals("1 d ago", Time.ago("2026-10-12T08:00:00+00:00", now))
    }

    @Test
    fun anUnparseableTimestampSaysNothingRatherThanInvalidDate() {
        assertEquals("", Time.ago("not a timestamp", Instant.now()))
        assertEquals("", Time.ago("", Instant.now()))
    }

    @Test
    fun aClockSkewedCardDoesNotClaimToBeFromTheFuture() {
        val now = Instant.parse("2026-10-13T08:00:00Z")
        // The computer's clock can run ahead of the phone's; "-3 min ago" would be
        // worse than saying nothing.
        assertEquals("", Time.ago("2026-10-13T08:05:00+00:00", now))
    }
}
