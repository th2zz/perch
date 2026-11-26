package dev.perch.console

import android.app.Application
import android.net.ConnectivityManager
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.live.NetworkWatch
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import org.robolectric.shadows.ShadowNetwork

/**
 * The half of the recovery promise that has to be *wired* to work.
 *
 * [ReconnectPacerTest] proves the wait can be interrupted. This proves something
 * in the app actually does the interrupting — that a network coming back reaches
 * it. The two are worth testing apart because either one alone is useless: an
 * interruptible wait nobody interrupts is the same thirty-second stall, and a
 * callback that talks to nothing is a listener that does nothing.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class NetworkWatchTest {

    private val context: Application = ApplicationProvider.getApplicationContext()

    private fun manager() = context.getSystemService(ConnectivityManager::class.java)!!

    @Test
    fun aNetworkComingBackReachesTheCallback() {
        var calls = 0
        NetworkWatch.register(context) { calls++ }

        // What the platform does when Wi-Fi or cellular becomes usable: it tells
        // every registered callback. Firing all of them rather than assuming ours is
        // last keeps the test about the wiring rather than about the order a
        // Robolectric shadow happens to keep callbacks in (it is not insertion
        // order, which made this test pass on its own and fail in the suite).
        shadowOf(manager()).networkCallbacks.forEach { it.onAvailable(ShadowNetwork.newInstance(1)) }

        assertEquals("exactly the callback the app registered must hear about it", 1, calls)
    }

    @Test
    fun theWatchIsRegisteredByTheApplicationAndNotByAScreen() {
        // Robolectric has already created the Application — and therefore run
        // `onCreate` — before this body, and nothing but PerchApp registers a network
        // callback. That this list is not empty is what pins the *placement*: an
        // Activity would be the wrong home for it, because the phone changes networks
        // in a pocket with no screen open, which is the case SC-008 is about.
        assertTrue(
            "PerchApp.onCreate must register the network watch; without it the " +
                "thirty-second backoff is waited out with the network already working",
            shadowOf(manager()).networkCallbacks.isNotEmpty(),
        )
    }
}
