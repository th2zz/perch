package dev.perch.console.features.queue

import android.content.Context
import java.io.IOException
import dev.perch.console.PerchApp
import dev.perch.console.core.herdrclient.BridgeException
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.herdrclient.DecisionRequest
import dev.perch.console.core.errors.FailureKind
import dev.perch.console.core.errors.Failures
import dev.perch.console.core.outbox.QueuedReply

/** What happened when the user decided something. */
sealed interface DecisionOutcome {
    /** The computer accepted it and the equivalent herdr command ran. */
    data class Handled(val equivalentCommand: List<String>, val secondsSaved: Int) : DecisionOutcome

    /**
     * The computer refused, and said why.
     *
     * `fix` is not decoration: every refusal here has a next step for the user
     * (approve an unmappable prompt from the computer, re-pair, wake the machine),
     * and dropping it would leave them stuck with a message and nothing to do.
     */
    data class Refused(val message: String, val fix: String) : DecisionOutcome

    /**
     * The computer did not answer at all.
     *
     * Kept apart from [Refused] because the two demand opposite treatment: a
     * refusal is final and must be shown, whereas a silence should be held and
     * retried. Collapsing them would either lose the user's reply or retry an
     * action the computer has already rejected.
     */
    data class Unreachable(val reason: String) : DecisionOutcome

    val retryable: Boolean
        get() = this is Unreachable
}

/** What to do with something that was waiting for the computer. */
enum class Disposition {
    /** The computer took it; it is done and gone. */
    Delivered,

    /** The computer answered, and the answer was no. Final, and it leaves. */
    Settled,

    /**
     * The computer did not answer. Keep it, and stop trying the rest of the list:
     * they are queued behind the same silence, so "one attempt per entry" would be
     * several timeouts for one unreachable computer.
     */
    KeepForLater,
}

/**
 * The policy the offline queue runs on, as a pure function.
 *
 * Written here rather than inline in the flush loop so it can be tested without a
 * device, a network or a computer: the difference between [Disposition.Delivered],
 * [Disposition.Settled] and [Disposition.KeepForLater] is the whole of what the user
 * experiences when their laptop was closed.
 */
fun dispositionOf(outcome: DecisionOutcome): Disposition = when (outcome) {
    is DecisionOutcome.Handled -> Disposition.Delivered
    is DecisionOutcome.Refused -> Disposition.Settled
    is DecisionOutcome.Unreachable -> Disposition.KeepForLater
}

/**
 * The one path a decision takes, whoever started it.
 *
 * A notification action and a tap inside the app must do exactly the same thing
 * — that is the whole point of "action equivalence" at the product level, not
 * just at the herdr level. Having one function means the two entry points cannot
 * drift apart, and it means the failure story is written once.
 */
object DecisionExecutor {

    /**
     * Submits a decision, holding it if the computer cannot be reached.
     *
     * The hold is applied here rather than by each caller on purpose. Two callers
     * already exist — the console screen and the notification action — and a
     * behaviour this important must not be something each of them remembers to
     * do. A reply typed into a notification while the laptop is asleep is exactly
     * as deserving of being kept as one typed in the app.
     */
    suspend fun run(
        context: Context,
        decisionId: String,
        paneId: String,
        kind: DecisionKind,
        text: String? = null,
        host: String = "",
    ): DecisionOutcome {
        val container = (context.applicationContext as PerchApp).container
        val client = container.client()
            ?: run {
                // Not paired at all: that is not "later", it is a setup problem,
                // and queueing here would silently pile up replies that can never
                // be delivered by waiting.
                return DecisionOutcome.Refused(
                    message = context.getString(dev.perch.console.R.string.failure_not_paired),
                    fix = context.getString(dev.perch.console.R.string.failure_not_paired_fix),
                )
            }

        val outcome = try {
            val response = client.decide(
                DecisionRequest(
                    decisionId = decisionId,
                    kind = kind.wire,
                    target = paneId,
                    text = text,
                    host = host,
                ),
            )
            DecisionOutcome.Handled(response.equivalentCommand, response.secondsSaved)
        } catch (e: Exception) {
            // One place decides whether this was "no" or "not now". A socket that
            // never opened, a timeout, a network that changed under us: all of
            // them mean later. A refusal means the computer has already answered,
            // and retrying it would only ask the same question again.
            when (val kind = Failures.classify(e)) {
                FailureKind.Unreachable -> DecisionOutcome.Unreachable(
                    context.getString(dev.perch.console.R.string.failure_unreachable),
                )
                else -> DecisionOutcome.Refused(
                    message = (e as? BridgeException)?.message
                        ?: context.getString(dev.perch.console.R.string.failure_refused),
                    fix = (e as? BridgeException)?.fix?.takeIf { it.isNotBlank() }
                        ?: context.getString(dev.perch.console.R.string.failure_refused_fix),
                )
            }
        }

        if (outcome.retryable) {
            container.outbox.enqueue(
                QueuedReply(
                    decisionId = decisionId,
                    paneId = paneId,
                    kind = kind.wire,
                    host = host,
                    text = text,
                    queuedAtMillis = System.currentTimeMillis(),
                ),
            )
        }
        return outcome
    }
}
