package dev.perch.console

import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.features.queue.PerchNotifications
import dev.perch.console.features.queue.TappedDecision
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The tap that the whole product is named after.
 *
 * Four strings from an intent become the arguments of a write that presses a key on
 * the user's computer. Getting one of them wrong is not a crash and not a visible
 * error: the button acts on the wrong request, or on the wrong machine, and the user
 * finds out later. So the mapping is a pure function, and this is its test.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class TappedDecisionTest {

    private fun intent(
        action: String = PerchNotifications.ACTION_APPROVE,
        decisionId: String? = "workbox/wA:p1@1",
        paneId: String? = "wA:p1",
        host: String? = null,
    ) = Intent(action).apply {
        decisionId?.let { putExtra(PerchNotifications.EXTRA_DECISION_ID, it) }
        paneId?.let { putExtra(PerchNotifications.EXTRA_PANE_ID, it) }
        host?.let { putExtra(PerchNotifications.EXTRA_HOST, it) }
    }

    @Test
    fun approveAndRejectAreTheOnlyTapsItUnderstands() {
        assertEquals(
            DecisionKind.APPROVE,
            TappedDecision.from(intent(action = PerchNotifications.ACTION_APPROVE))?.kind,
        )
        assertEquals(
            DecisionKind.REJECT,
            TappedDecision.from(intent(action = PerchNotifications.ACTION_REJECT))?.kind,
        )
        // Anything else — a tap on the body of the notification (which opens the app
        // through a content intent, not an action), or an action someone adds later —
        // is not a decision and must not be read as one.
        assertNull(TappedDecision.from(intent(action = Intent.ACTION_VIEW)))
    }

    @Test
    fun everyFieldLandsInItsOwnArgument() {
        val tapped = TappedDecision.from(
            intent(decisionId = "workbox/wA:p1@7", paneId = "wA:p1", host = "workbox"),
        )!!
        assertEquals("workbox/wA:p1@7", tapped.decisionId)
        assertEquals("wA:p1", tapped.paneId)
        // The host travels with the pane: `wA:p1` exists on every computer, so a
        // swapped or dropped host is a key pressed on somebody else's machine.
        assertEquals("workbox", tapped.host)
    }

    @Test
    fun aTapForThisComputerCarriesNoHost() {
        // Absent is the common case (a single machine, or a notification from before
        // multi-host existed), and it must mean "this computer" rather than nothing.
        assertEquals("", TappedDecision.from(intent(host = null))?.host)
    }

    @Test
    fun anIncompleteTapIsIgnoredRatherThanHalfActed() {
        assertNull(TappedDecision.from(intent(decisionId = null)))
        assertNull(TappedDecision.from(intent(paneId = null)))
        assertNull(TappedDecision.from(intent(decisionId = "")))
        assertNull(TappedDecision.from(intent(paneId = "   ")))
        // A decision id and a pane are what make the write possible; without them
        // there is nothing to do and nothing to guess.
        assertNull(TappedDecision.from(Intent()))
    }
}
