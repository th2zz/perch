package dev.perch.console.features.queue

import android.content.Intent
import dev.perch.console.core.herdrclient.DecisionKind

/**
 * What a tap on a notification button means, read out of its intent.
 *
 * This is a handful of lines lifted out of the receiver for one reason: it is the
 * only place where four strings from an intent become the arguments of a write that
 * presses a key on somebody's computer. A swapped `pane_id` and `host`, or a renamed
 * extra, is not a compile error and not a visible one — the button simply acts on the
 * wrong thing — so the mapping gets to be a pure function with its own test.
 */
data class TappedDecision(
    val decisionId: String,
    val paneId: String,
    val host: String,
    val kind: DecisionKind,
) {
    companion object {
        /**
         * Reads one tap, or null when the intent is not one of ours.
         *
         * Null rather than a half-filled value: an intent missing its decision or its
         * pane cannot be acted on, and the receiver's job in that case is to do
         * nothing at all.
         */
        fun from(intent: Intent): TappedDecision? {
            val kind = when (intent.action) {
                PerchNotifications.ACTION_APPROVE -> DecisionKind.APPROVE
                PerchNotifications.ACTION_REJECT -> DecisionKind.REJECT
                else -> return null
            }
            val decisionId = intent.getStringExtra(PerchNotifications.EXTRA_DECISION_ID)
                ?.takeIf { it.isNotBlank() } ?: return null
            val paneId = intent.getStringExtra(PerchNotifications.EXTRA_PANE_ID)
                ?.takeIf { it.isNotBlank() } ?: return null
            // The host is allowed to be absent: a request from this computer is the
            // common case, and older notifications carry no host at all.
            val host = intent.getStringExtra(PerchNotifications.EXTRA_HOST).orEmpty()
            return TappedDecision(decisionId, paneId, host, kind)
        }
    }
}
