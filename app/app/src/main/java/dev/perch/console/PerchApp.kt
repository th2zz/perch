package dev.perch.console

import android.app.Application
import dev.perch.console.core.live.BridgeStream
import dev.perch.console.core.live.NetworkWatch

/**
 * Holds the app container.
 *
 * There is no background service here on purpose. The phone does not need to run
 * anything to be reachable: notifications arrive through the platform's push
 * channel, and the user's computer does the waiting. A foreground service would
 * cost battery and show a permanent notification to buy reliability the push
 * path already provides.
 */
class PerchApp : Application() {
    val container: AppContainer by lazy { AppContainer(this) }

    override fun onCreate() {
        super.onCreate()
        // Recovery is the one promise that is judged while nobody is looking: the
        // phone changes networks in a pocket, and the user finds out whether we
        // noticed only when they next open the app. Reacting to the platform's
        // "there is a network again" is what makes SC-008's five seconds a
        // property of the design rather than of how the timing happened to fall.
        NetworkWatch.register(this) { BridgeStream.retryNow() }
    }
}
