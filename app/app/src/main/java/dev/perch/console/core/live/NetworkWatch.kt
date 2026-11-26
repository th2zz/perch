package dev.perch.console.core.live

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network

/**
 * Tells us when a network comes back.
 *
 * The app already notices the *symptom*: a socket that stopped working. What it
 * could not notice is the *cause ending*, and those are different events — the
 * socket can be dead for minutes before anything tries to use it, and the backoff
 * timer may have climbed to its cap in the meantime. Without this, SC-008's
 * "switch networks and recover within five seconds" is met by luck of timing
 * rather than by design; with it, the moment the platform has a usable network is
 * the moment we try again.
 *
 * `registerDefaultNetworkCallback` rather than watching for a specific network: the
 * phone moving from Wi-Fi to cellular is exactly the case this exists for, and both
 * are "usable" as far as we are concerned. Firing on a network that turns out not to
 * reach the computer costs nothing — the attempt fails and the backoff resumes.
 *
 * The registration is deliberately never unregistered. It belongs to the process,
 * not to a screen: the watching service holds the connection while the app is
 * closed, and it is the case where the phone is in a pocket, changing networks with
 * nobody looking, that this is for.
 */
object NetworkWatch {

    /**
     * Registers [onAvailable] to run whenever a usable network appears.
     *
     * A missing system service is not an error: this is an optimisation on a
     * recovery path that still works without it (more slowly). Failing the app to
     * start because a device would not hand over its connectivity manager would be
     * trading a small latency win for a crash.
     */
    fun register(context: Context, onAvailable: () -> Unit) {
        val manager = context.getSystemService(ConnectivityManager::class.java) ?: return
        runCatching {
            manager.registerDefaultNetworkCallback(
                object : ConnectivityManager.NetworkCallback() {
                    override fun onAvailable(network: Network) = onAvailable()
                },
            )
        }
    }
}
