package dev.perch.console.features.queue

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import dev.perch.console.PerchApp

/**
 * Puts the watching service back after a reboot.
 *
 * Without this, "the phone tells you when an agent stops" would quietly stop
 * being true the next time the phone restarts — and the user would only find out
 * by missing a decision. The service is only restarted if the user is still
 * paired, so a phone that was never set up does not get a service.
 */
class BootReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        val container = (context.applicationContext as PerchApp).container
        if (!shouldRestartWatching(intent.action, paired = container.pairingStore.load() != null)) {
            return
        }
        BridgeWatchService.start(context)
    }
}

/**
 * Whether this broadcast should put the watching service back.
 *
 * Pulled out of the receiver so the rule can be tested without a device: it is the
 * whole content of the promise ("ask once, then keep telling me, including after a
 * restart"), and it runs on a broadcast that arrives with the app not running, where a
 * mistake is invisible until a decision is missed.
 *
 * Two actions matter and they are the two moments the service is *not* running and
 * should be: the phone rebooted, and the app was replaced.
 */
internal fun shouldRestartWatching(action: String?, paired: Boolean): Boolean {
    if (!paired) return false
    return action == Intent.ACTION_BOOT_COMPLETED || action == Intent.ACTION_MY_PACKAGE_REPLACED
}
