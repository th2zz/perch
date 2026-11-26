package dev.perch.console

import dev.perch.console.core.format.Duration
import dev.perch.console.core.herdrclient.ValueResponse
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The product's claims about the user's time.
 *
 * These numbers are the reason someone pays, which is exactly why they get
 * tested: a flattering figure that quietly changes meaning is the fastest way to
 * lose a technical user's trust.
 */
class ValueClaimTest {

    private companion object {
        // One instance: kotlinx.serialization warns on repeated construction, and
        // a singleton is also what the app itself does.
        val json = Json { ignoreUnknownKeys = true }
    }

    @Test
    fun secondsBecomeSomethingSomeoneWouldSay() {
        assertEquals("nothing yet", Duration.humanise(0))
        assertEquals("nothing yet", Duration.humanise(-5))
        assertEquals("45 seconds", Duration.humanise(45))
        // A fragment, not a sentence: the hedge belongs to whatever says it
        // ("About … back this week", "Roughly … you did not spend"), so that no
        // screen has to repair the wording with string surgery.
        assertEquals("a minute", Duration.humanise(60))
        assertEquals("8 minutes", Duration.humanise(8 * 60))
        assertEquals("an hour", Duration.humanise(60 * 60))
        assertEquals("2 hours", Duration.humanise(2 * 60 * 60))
        assertEquals("an hour and 20 minutes", Duration.humanise(80 * 60))
    }

    @Test
    fun aSingleDecisionDoesNotClaimMoreThanItIs() {
        // The baseline is 90 seconds. Under a minute it must not round up into a
        // minute, and it must not say "saved" — it says the user did not spend
        // that time walking back, which is what was actually measured.
        val claim = Duration.savedClaim(90)
        assertTrue(claim.contains("did not spend"))
        assertTrue("it should read as time, not as seconds: $claim", claim.contains("minute"))
    }

    @Test
    fun shortDecisionsStayInSeconds() {
        assertTrue(Duration.savedClaim(20).contains("20 seconds"))
    }

    @Test
    fun theWeeklyTotalsDecodeAsTheBridgeWritesThem() {
        // The bridge computes these figures; the app only renders them. The
        // contract between the two is this JSON, so it is asserted rather than
        // assumed — a renamed field would otherwise reach users as a card that
        // silently says "0 decisions" for ever.
        val wire = """
            {"week":{"decisions":3,"not_at_computer":2,"seconds_saved":270,"baseline_seconds":90}}
        """.trimIndent()

        val stats = json.decodeFromString<ValueResponse>(wire).week

        assertEquals(3, stats.decisions)
        assertEquals(2, stats.notAtComputer)
        assertEquals(270, stats.secondsSaved)
        // FR-013: the estimate must be explainable, and the explanation is the
        // baseline itself. The bridge sends it; the card shows it.
        assertEquals(90, stats.baselineSeconds)
    }
}
