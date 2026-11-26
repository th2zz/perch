package dev.perch.console.features.queue

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch

/**
 * Handles Approve / Reject tapped straight on the notification.
 *
 * This is the interaction the product exists for — the user is away from the
 * desk and answers without opening anything — so it must work with the app in
 * the background and the screen locked.
 *
 * `goAsync` gives the receiver a short window to finish a network call after
 * `onReceive` has returned. That is the supported way to do this: the
 * alternative, spawning a background service, is restricted on modern Android
 * and would fail precisely when the user needs it.
 */
class DecisionActionReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        // Reading the tap is a pure function with tests (see TappedDecision): four
        // strings becoming the arguments of a write is exactly the kind of mapping
        // that fails silently.
        val tapped = TappedDecision.from(intent) ?: return
        val appContext = context.applicationContext

        val pending = goAsync()
        CoroutineScope(Dispatchers.IO).launch {
            try {
                when (
                    val outcome = DecisionExecutor.run(
                        context = appContext,
                        decisionId = tapped.decisionId,
                        paneId = tapped.paneId,
                        kind = tapped.kind,
                        host = tapped.host,
                    )
                ) {
                    is DecisionOutcome.Handled -> {
                        // Only now is it safe to remove the card: the computer has
                        // confirmed the command ran.
                        PerchNotifications.cancel(appContext, tapped.decisionId)
                    }
                    is DecisionOutcome.Refused -> {
                        // Keep the card and say what happened. A silent failure
                        // would leave the user believing the agent was unblocked.
                        PerchNotifications.problem(appContext, outcome.message, outcome.fix)
                    }
                    is DecisionOutcome.Unreachable -> {
                        // The reply is already held by DecisionExecutor, so the
                        // card stays and the user is told it will go later rather
                        // than being told it failed.
                        PerchNotifications.problem(
                            appContext,
                            appContext.getString(dev.perch.console.R.string.reply_queued),
                            appContext.getString(dev.perch.console.R.string.reply_queued_fix),
                        )
                    }
                }
            } finally {
                pending.finish()
            }
        }
    }
}
