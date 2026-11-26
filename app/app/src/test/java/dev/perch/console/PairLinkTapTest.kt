package dev.perch.console

import android.app.Application
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import androidx.test.core.app.ApplicationProvider
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The tap that is supposed to cost nothing.
 *
 * Zero-typing onboarding is a promise about a *tap*: the computer prints one line,
 * the user sends it to themselves, taps it in whatever chat app, and the app opens
 * with the address and the secret already in hand. `PairLinkFixtureTest` proves the
 * app can *parse* the link — but parsing only happens if the tap reaches the app at
 * all, and that is decided by Android from the manifest's `intent-filter`, not by
 * any Kotlin code. A filter with the wrong scheme, a missing `BROWSABLE` category,
 * or an `exported="false"` activity behaves exactly like a parser that does not
 * work: the user taps and nothing happens.
 *
 * So this test resolves a real intent the way a chat app would, against the merged
 * manifest, and requires it to land on this app's activity — using the *bridge's
 * own links* from the shared fixture rather than a literal typed here. If the
 * filter and the link ever disagree, the first step of onboarding is broken, and it
 * is broken in the one place nothing else in the test suite looks.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PairLinkTapTest {

    @Serializable
    private data class FixtureCase(val why: String = "", val link: String)

    private val context: Application = ApplicationProvider.getApplicationContext()

    private val json = Json { ignoreUnknownKeys = true }

    private fun links(): List<FixtureCase> = json.decodeFromString(
        ContractFixtures.text(
            "contracts/pairlink-cases.json",
            "PERCH_UPDATE_PAIRLINK_FIXTURE=1 go test ./internal/pairlink/ -run Fixture",
        ),
    )

    /** Resolves a link the way the system would when the user taps it. */
    private fun resolverFor(link: String) = context.packageManager.resolveActivity(
        Intent(Intent.ACTION_VIEW, Uri.parse(link)).addCategory(Intent.CATEGORY_BROWSABLE),
        PackageManager.MATCH_DEFAULT_ONLY,
    )

    @Test
    fun everyLinkTheBridgePrintsOpensTheAppWhenItIsTapped() {
        val links = links()
        assertTrue("the shared pairing fixture is empty", links.isNotEmpty())

        for (case in links) {
            val resolved = resolverFor(case.link)
            assertEquals(
                "tapping this link would not open the app (${case.why}): ${case.link}\n" +
                    "the manifest's intent-filter and the bridge's scheme/host have to agree, " +
                    "and only this test compares them",
                MainActivity::class.java.name,
                resolved?.activityInfo?.name,
            )
        }
    }

    @Test
    fun theAppIsNotAGeneralPurposeLinkHandler() {
        // The filter is deliberately narrow. If it ever widened to all https links
        // the app would offer itself for every link the user taps anywhere, and
        // "install this and nothing about your phone changes" would be false.
        for (innocent in listOf("https://example.com/", "perch://settings?x=1", "not-a-link")) {
            val resolved = resolverFor(innocent)
            assertNull(
                "the app must not claim to handle $innocent",
                resolved?.activityInfo?.name,
            )
        }
    }
}
