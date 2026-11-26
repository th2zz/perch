package dev.perch.console

import dev.perch.console.core.auth.PairLink
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The app's half of the *shared* pairing contract.
 *
 * [PairLinkTest] checks the parser against literals a person typed. That catches
 * regressions inside this file, but it cannot catch the failure that matters most:
 * the bridge changing what it prints while both test suites stay green. The bytes
 * now live in one file that the Go encoder writes and this test reads —
 * `contracts/pairlink-cases.json` — so a drift on either
 * side of the seam turns a test red instead of turning the user's first tap into
 * nothing.
 *
 * A plain JVM test: the parser uses java.net.URI, so this runs the real code with
 * no device and no robotised approximation of a URI.
 */
class PairLinkFixtureTest {

    @Serializable
    private data class FixtureCase(
        val why: String = "",
        val address: String,
        val token: String,
        val name: String = "",
        val link: String,
    )

    private val json = Json { ignoreUnknownKeys = true }

    /**
     * The checkout root, found by walking up rather than by a configured path.
     *
     * Gradle runs unit tests with the module directory as the working directory,
     * and pinning a relative `../../..` would break the moment the task changes
     * that. `.git` is the marker, and not finding it is a failure rather than a
     * skip: a test that silently does nothing is worse than no test.
     */
    private fun cases(): List<FixtureCase> = json.decodeFromString(
        ContractFixtures.text(
            "contracts/pairlink-cases.json",
            "PERCH_UPDATE_PAIRLINK_FIXTURE=1 go test ./internal/pairlink/ -run Fixture",
        ),
    )

    @Test
    fun everyLinkTheBridgePrintsIsALinkTheAppCanRead() {
        val cases = cases()
        // A blank fixture would make every assertion below vacuous, and a blank
        // fixture is exactly what a bad regeneration produces.
        assertTrue("the shared pairing fixture is empty", cases.isNotEmpty())

        for (case in cases) {
            val invite = PairLink.parse(case.link)
            assertNotNull("the app could not read the link the bridge prints (${case.why}): ${case.link}", invite)
            assertEquals("address (${case.why})", case.address, invite!!.address)
            assertEquals("token (${case.why})", case.token, invite.token)
            assertEquals("computer name (${case.why})", case.name, invite.computerName)

            // The intent filter decides whether the tap ever reaches the parser,
            // and it leans on this prefix check, so it is pinned against the same
            // bytes rather than against a hand-typed literal.
            assertTrue(
                "the app would not even recognise this link as an invite (${case.why}): ${case.link}",
                PairLink.looksLikeInvite(case.link),
            )
        }
    }
}
