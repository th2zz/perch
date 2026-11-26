package dev.perch.console

import android.app.Application
import android.content.Context
import android.content.Intent
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.auth.Pairing
import dev.perch.console.features.queue.BootReceiver
import dev.perch.console.features.queue.shouldRestartWatching
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * The promise that survives a restart.
 *
 * "Your phone tells you when an agent stops" has to keep being true after the phone
 * reboots, and the only thing that makes it true is this receiver. If it silently
 * stopped, nothing would break — the app would look correct, the bridge would be
 * running, and the user would find out by missing a decision hours later.
 *
 * The pairing check is the other half: a phone that was never set up must not get a
 * foreground service with a permanent notification.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class BootReceiverTest {

    private val context: Application = ApplicationProvider.getApplicationContext()

    private fun pair(address: String? = "100.64.0.1:45999") {
        val prefs = context.getSharedPreferences("perch.pairing", Context.MODE_PRIVATE)
        prefs.edit().apply {
            if (address == null) remove("address") else putString("address", address)
        }.commit()
    }

    private fun boot(action: String = Intent.ACTION_BOOT_COMPLETED) {
        BootReceiver().onReceive(context, Intent(action))
    }

    // The rule, over its whole input space.
    //
    // A paired phone must come back watching after a reboot *and* after an app update —
    // those are the two moments the service is not running and should be — and on
    // nothing else. The broadcast arrives with the app not running, so a mistake here
    // is invisible until a decision is missed.
    @Test
    fun theRuleIsExactlyThoseTwoMoments() {
        for (action in listOf(
            Intent.ACTION_BOOT_COMPLETED,
            Intent.ACTION_MY_PACKAGE_REPLACED,
        )) {
            assertTrue("$action must restart watching", shouldRestartWatching(action, paired = true))
            assertFalse(
                "an unpaired phone must not get a service on $action",
                shouldRestartWatching(action, paired = false),
            )
        }
        for (action in listOf(
            Intent.ACTION_POWER_CONNECTED,
            Intent.ACTION_LOCALE_CHANGED,
            Intent.ACTION_USER_PRESENT,
            null,
        )) {
            assertFalse("$action is not a reason to start a service", shouldRestartWatching(action, paired = true))
        }
    }

    /**
     * And one end of it through the real receiver: a phone that was never set up must
     * not end up with a permanent notification.
     *
     * Only this direction can be checked here — Robolectric has no AndroidKeyStore, so a
     * "paired" phone cannot exist in this test process. That the paired case starts the
     * service is a one-line call into the service, exercised on a device.
     */
    @Test
    fun aPhoneThatIsNotPairedGetsNoService() {
        pair(address = null)
        boot()
        assertNull(
            "a phone that was never set up must not get a permanent notification",
            shadowOf(context).nextStartedService,
        )
    }
}
