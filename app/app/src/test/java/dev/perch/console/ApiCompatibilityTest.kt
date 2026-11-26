package dev.perch.console

import dev.perch.console.core.herdrclient.ApiCompatibility
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ApiCompatibilityTest {

    @Test
    fun theSameContractIsQuiet() {
        val verdict = ApiCompatibility.of(ApiCompatibility.API_VERSION)
        assertEquals(ApiCompatibility.Ok, verdict)
        assertFalse(verdict.isMismatch)
        assertEquals(null, verdict.messageResource())
    }

    @Test
    fun anOlderBridgeSaysSoAndNamesTheHalfToUpdate() {
        // One contract behind is now representable, and the sentence must name the bridge.
        val verdict = ApiCompatibility.of(bridgeApiVersion = 1, appApiVersion = 2)
        assertEquals(ApiCompatibility.BridgeTooOld, verdict)
        assertTrue(verdict.isMismatch)
        assertEquals(R.string.failure_bridge_too_old, verdict.messageResource())
        // Which half to update is the whole value of the sentence.
        assertEquals(R.string.failure_bridge_too_old_fix, verdict.fixResource())
    }

    @Test
    fun anOlderAppSaysSoToo() {
        val verdict = ApiCompatibility.of(ApiCompatibility.API_VERSION + 1)
        assertEquals(ApiCompatibility.AppTooOld, verdict)
        assertEquals(R.string.failure_app_too_old, verdict.messageResource())
        assertEquals(R.string.failure_app_too_old_fix, verdict.fixResource())
    }

    @Test
    fun theContractIsAtVersion2AndZeroIsUnknown() {
        // Pins the arithmetic: if the contract is ever bumped, this expectation and the
        // bridge's version.go are the two things that must change deliberately.
        assertEquals(2, ApiCompatibility.API_VERSION)
        assertEquals(ApiCompatibility.Unknown, ApiCompatibility.of(0))
    }

    @Test
    fun aComputerThatDidNotSayIsNotAccused() {
        // An older bridge has no such field. Calling that "incompatible" would be a
        // guess, and a guess that blocks a working setup is worse than trying the
        // request and reporting whatever the computer actually answers.
        val verdict = ApiCompatibility.of(0)
        assertEquals(ApiCompatibility.Unknown, verdict)
        assertFalse(verdict.isMismatch)
        assertEquals(null, verdict.messageResource())
    }
}
