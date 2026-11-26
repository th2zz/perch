package dev.perch.console

import dev.perch.console.core.live.ReconnectPacer
import kotlinx.coroutines.launch
import kotlinx.coroutines.test.advanceTimeBy
import kotlinx.coroutines.test.advanceUntilIdle
import kotlinx.coroutines.test.runCurrent
import kotlinx.coroutines.test.runTest
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * SC-008: "the session recovers within five seconds of losing the network **or
 * switching networks**".
 *
 * The reconnect loop backs off exponentially up to thirty seconds, and that is the
 * right shape for "the computer is asleep" — hammering a machine that is off helps
 * nobody. It is the wrong shape for "the network came back", because the timer does
 * not measure the outage, it measures our own attempts: a phone that spent the
 * night out of signal wakes up with the backoff sitting at its cap, so the
 * connection stays idle for up to half a minute while the network is perfectly
 * fine. Nothing is broken and it looks broken, for exactly as long as a person will
 * wait before concluding the app is.
 *
 * These tests run on virtual time, so they pin the policy — how long a wait lasts
 * and what it reports — with no device, no network, and no waiting.
 */
class ReconnectPacerTest {

    @Test
    fun theLongestWaitEndsTheMomentTheNetworkIsBack() {
        runTest {
            val pacer = ReconnectPacer()
            var outcome: Boolean? = null
            val waiter = launch { outcome = pacer.wait(30_000) }

            // A person picking the phone up, two seconds into the thirty.
            advanceTimeBy(2_000)
            runCurrent()
            assertTrue("the wait should still be running", !waiter.isCompleted)

            pacer.nudge()
            advanceUntilIdle()

            assertTrue("the nudge must end the wait", waiter.isCompleted)
            assertTrue("and it must say so, so the caller restarts from the fast retry", outcome == true)
        }
    }

    @Test
    fun withNothingToSayTheWaitRunsItsFullLength() {
        runTest {
            val pacer = ReconnectPacer()
            var outcome: Boolean? = null
            val waiter = launch { outcome = pacer.wait(30_000) }

            advanceTimeBy(29_999)
            runCurrent()
            assertFalse("a backoff must not end early on its own", waiter.isCompleted)

            advanceTimeBy(2)
            runCurrent()
            assertTrue("the wait ends when the time is up", waiter.isCompleted)
            assertFalse("nothing nudged it, so the caller must not reset its backoff", outcome == true)
        }
    }

    @Test
    fun aBurstOfNetworksIsOneReasonToRetry() {
        runTest {
            val pacer = ReconnectPacer()
            // Wi-Fi and cellular both coming up can arrive as several callbacks.
            // They mean one thing, and a queue of nudges would turn one event into a
            // burst of reconnects.
            pacer.nudge()
            pacer.nudge()
            pacer.nudge()
            assertTrue("the first wait is ended by the burst", pacer.wait(30_000))

            var outcome: Boolean? = null
            val second = launch { outcome = pacer.wait(30_000) }
            advanceTimeBy(30_001)
            runCurrent()
            assertTrue("the next wait is a fresh one", second.isCompleted)
            assertFalse("the coalesced nudges must not leak into the next wait", outcome == true)
        }
    }
}
