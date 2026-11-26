package dev.perch.console

import android.content.Context
import dev.perch.console.core.auth.Pairing
import dev.perch.console.core.auth.PairingStore
import dev.perch.console.core.cache.MirrorCache
import dev.perch.console.core.herdrclient.BridgeClient
import dev.perch.console.core.settings.Settings
import dev.perch.console.core.voice.VoiceSettings
import dev.perch.console.core.outbox.Outbox
import java.io.File
import kotlinx.serialization.json.Json

/**
 * The app's dependencies, wired by hand.
 *
 * A dependency-injection framework would be the reflex here, and it would buy
 * nothing: the graph is a preferences handle, a cache file and one HTTP client.
 * Hand-wiring keeps the object graph readable in a single screenful, which
 * matters more for a codebase this size than a generated one.
 */
class AppContainer(context: Context) {
    private val appContext = context.applicationContext

    val pairingStore = PairingStore(appContext)

    val settings = Settings(appContext)

    val voiceSettings = VoiceSettings(appContext)

    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
    }

    private val cache = MirrorCache(File(appContext.filesDir, "mirror.json"), json)

    val outbox = Outbox(File(appContext.filesDir, "outbox.json"), json)

    /** Builds a client for the paired computer, or null when unpaired. */
    fun client(): BridgeClient? {
        val pairing: Pairing = pairingStore.load() ?: return null
        val url = BridgeClient.parseAddress(pairing.address) ?: return null
        return BridgeClient(url, pairing.token)
    }

    fun cachedState() = cache.read()

    fun cacheState(state: dev.perch.console.core.cache.CachedState) = cache.write(state)

    fun forgetPairing() {
        cache.clear()
        outbox.clear()
        // Recent folders are paths on the computer this phone was paired with. Keeping them after
        // the pairing is gone would offer the next computer a list of places it does not have.
        settings.clearRecentFolders()
    }
}
