package dev.perch.console

import dev.perch.console.features.queue.DecisionOutcome
import dev.perch.console.features.queue.Disposition
import dev.perch.console.features.queue.dispositionOf
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OfflineQueuePolicyTest {

    @Test
    fun silenceIsHeld() {
        val outcome = DecisionOutcome.Unreachable("your computer did not answer")
        assertTrue("a silence must be retried later or the sentence is lost", outcome.retryable)
        assertEquals(Disposition.KeepForLater, dispositionOf(outcome))
    }

    @Test
    fun aRefusalIsFinal() {
        val outcome = DecisionOutcome.Refused("the computer would not do that", "answer it there")
        assertFalse("a refusal is an answer, not a silence", outcome.retryable)
        // Settled rather than Delivered: it leaves the queue either way, but the user
        // is owed the reason, and this is where that distinction lives.
        assertEquals(Disposition.Settled, dispositionOf(outcome))
    }

    @Test
    fun aDeliveredReplyLeaves() {
        val outcome = DecisionOutcome.Handled(listOf("herdr", "agent", "send-keys", "wA:p1", "enter"), 90)
        assertFalse(outcome.retryable)
        assertEquals(Disposition.Delivered, dispositionOf(outcome))
    }

    @Test
    fun everyOutcomeIsAccountedFor() {
        // Three outcomes, three dispositions, no default branch: a fourth outcome
        // added later must be given an answer deliberately rather than inheriting
        // "hold it" or "drop it" by falling through.
        val outcomes = listOf(
            DecisionOutcome.Handled(emptyList(), 0),
            DecisionOutcome.Refused("no", "fix"),
            DecisionOutcome.Unreachable("silence"),
        )
        assertEquals(3, outcomes.toSet().size)
        assertEquals(
            listOf(Disposition.Delivered, Disposition.Settled, Disposition.KeepForLater),
            outcomes.map(::dispositionOf),
        )
    }
}
