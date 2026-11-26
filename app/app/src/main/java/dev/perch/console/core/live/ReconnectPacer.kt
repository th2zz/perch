package dev.perch.console.core.live

import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Waits out a reconnect backoff — unless something says the reason for waiting is
 * over.
 *
 * SC-008 says the session recovers "within five seconds of losing the network **or
 * switching networks**". A plain exponential backoff does not meet that, and the
 * way it fails is worth stating: the backoff does not measure the outage, it
 * measures our own attempts. A laptop asleep overnight leaves the timer at its
 * thirty-second cap, so when the user picks the phone up and the network is
 * already fine, the app still sits there for up to half a minute with the
 * connection sitting idle. Nothing is broken and it looks broken, for exactly the
 * length of a person's patience.
 *
 * So the wait is interruptible, and the thing that interrupts it is the platform
 * telling us a network came back ([NetworkWatch]). A nudge ends the wait
 * immediately, and [wait] reports that it did, which is how the caller knows to
 * start again from the fast retry rather than from where the backoff had climbed
 * to — a network that has come back is a new situation, not a continuation of the
 * old failure.
 *
 * The channel is conflated on purpose: several networks appearing in a burst is
 * one reason to retry, and a queue of nudges would only cause a burst of attempts.
 */
class ReconnectPacer {

    private val nudges = Channel<Unit>(Channel.CONFLATED)

    /** Says "the network is back; stop waiting". Safe to call from any thread. */
    fun nudge() {
        nudges.trySend(Unit)
    }

    /**
     * Suspends for [backoffMillis], or until a nudge arrives.
     *
     * @return true when a nudge ended the wait early. The caller resets its
     *   backoff when this is true and doubles it when it is false.
     */
    suspend fun wait(backoffMillis: Long): Boolean =
        withTimeoutOrNull(backoffMillis) { nudges.receive() } != null
}
