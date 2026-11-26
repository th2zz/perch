package dev.perch.console

import dev.perch.console.core.errors.FailureKind
import dev.perch.console.core.errors.Failures
import dev.perch.console.core.herdrclient.BridgeException
import java.io.IOException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The one distinction the product cannot get wrong.
 *
 * A refusal retried is an endless loop of the same answer. A silence dropped is a
 * sentence the user typed that never arrived, which is the most annoying way this
 * product could fail, and it fails exactly when they are away from their desk.
 */
class FailureTest {

    @Test
    fun aComputerThatDidNotAnswerMeansLater() {
        val silences = listOf(
            IOException("socket closed"),
            UnknownHostException("no route"),
            SocketTimeoutException("timed out"),
            // Our own client's shape for "never got a response".
            BridgeException("could not reach it", httpStatus = 0),
        )
        silences.forEach { error ->
            assertEquals(error.toString(), FailureKind.Unreachable, Failures.classify(error))
            assertTrue("a silence should be retried", Failures.worthRetrying(Failures.classify(error)))
        }
    }

    @Test
    fun aRefusalIsFinal() {
        val refusals = listOf(
            // The bridge could not map the action to deterministic keys.
            BridgeException("no deterministic key sequence", httpStatus = 422),
            BridgeException("unsupported decision kind", httpStatus = 400),
        )
        refusals.forEach { error ->
            val kind = Failures.classify(error)
            assertFalse("$error must not be retried", Failures.worthRetrying(kind))
        }
    }

    @Test
    fun aStaleRequestIsNotARetry() {
        // 404 and 409 mean the queue moved on. Retrying the same request would
        // ask a question that has already been answered, possibly on the computer.
        assertEquals(FailureKind.Stale, Failures.classify(BridgeException("gone", httpStatus = 404)))
        assertEquals(FailureKind.Stale, Failures.classify(BridgeException("resolved", httpStatus = 409)))
        assertFalse(Failures.worthRetrying(FailureKind.Stale))
    }

    @Test
    fun anExpiredPairingIsASetupProblemNotALater() {
        // Queueing a reply because the token was rotated would pile up messages
        // that waiting can never deliver.
        assertEquals(FailureKind.NotPaired, Failures.classify(BridgeException("unauthorised", httpStatus = 401)))
        assertFalse(Failures.worthRetrying(FailureKind.NotPaired))
    }

    @Test
    fun aBrokenComputerLooksLikeASilenceNotARefusal() {
        // 5xx is the computer failing, not the request being wrong, so the user's
        // sentence should be kept.
        assertEquals(FailureKind.Unreachable, Failures.classify(BridgeException("boom", httpStatus = 500)))
        assertTrue(Failures.worthRetrying(FailureKind.Unreachable))
    }

    @Test
    fun anythingElseIsReportedRatherThanGuessed() {
        assertEquals(FailureKind.Unknown, Failures.classify(IllegalStateException("?")))
        assertFalse(Failures.worthRetrying(FailureKind.Unknown))
    }
}
