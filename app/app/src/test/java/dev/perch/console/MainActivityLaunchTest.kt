package dev.perch.console

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * "The app starts" — the one thing every screen test takes for granted.
 *
 * The rest of the suite renders individual screens with `setContent`, which skips
 * everything between the launcher icon and the first frame: `PerchApp.onCreate`,
 * the theme from the manifest, the activity's own `onCreate`, and the ViewModel
 * being constructed against the real `AppContainer`. A crash anywhere in there
 * makes the product a black screen, and no unit test of a screen would notice —
 * on a device it is the very first thing that happens, and it is the failure that
 * would end a trial before it started.
 *
 * So this launches the real activity through the real manifest and asks for a
 * sentence from the first screen. It is a small test doing a large job: it fails
 * if the app cannot start, which nothing else here can say.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class MainActivityLaunchTest {

    @get:Rule
    val compose = createAndroidComposeRule<MainActivity>()

    @Test
    fun theAppStartsAndShowsTheFirstScreen() {
        // An unpaired phone: the pairing screen, addressed to the user in the words
        // the onboarding actually uses. Taken from the resource rather than typed
        // here, so changing the copy cannot make this test pass while the screen
        // shows something else.
        val context = ApplicationProvider.getApplicationContext<PerchApp>()
        val headline = context.getString(R.string.pair_title)

        compose.onNodeWithText(headline).assertIsDisplayed()
    }

    @Test
    fun startingTheAppRegistersTheNetworkWatch() {
        // The cold start is also the moment the app decides to react to the network.
        // Asserting it here rather than only in the unit test of the helper is what
        // ties "the promise" to "the thing that actually happens when a user opens
        // the app": this is the real Application, created by the real launch.
        val manager = ApplicationProvider.getApplicationContext<PerchApp>()
            .getSystemService(android.net.ConnectivityManager::class.java)!!
        assertTrue(
            "the app must watch for the network coming back as soon as it starts",
            shadowOf(manager).networkCallbacks.isNotEmpty(),
        )
    }
}
