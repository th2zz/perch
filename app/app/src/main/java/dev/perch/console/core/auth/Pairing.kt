package dev.perch.console.core.auth

import android.content.Context
import androidx.core.content.edit
import okhttp3.HttpUrl

/**
 * The user's own computer, as this phone knows it.
 *
 * `address` is not a secret — it is an IP on the user's own network — so it
 * lives in ordinary preferences. `token` is the secret and goes through
 * [SecureStore]. Keeping them separate means the pairing screen can show the
 * address again without decrypting anything.
 */
data class Pairing(
    val address: String,
    val token: String,
)

class PairingStore(private val context: Context) {
    fun load(): Pairing? {
        val address = plainPrefs().getString(KEY_ADDRESS, null) ?: return null
        val token = SecureStore.get(context, KEY_TOKEN) ?: return null
        if (address.isBlank() || token.isBlank()) return null
        return Pairing(address, token)
    }

    fun save(pairing: Pairing) {
        plainPrefs().edit { putString(KEY_ADDRESS, pairing.address) }
        SecureStore.put(context, KEY_TOKEN, pairing.token)
    }

    fun clear() {
        plainPrefs().edit { remove(KEY_ADDRESS) }
        SecureStore.remove(context, KEY_TOKEN)
    }

    /**
     * A user-facing label for the computer. Falls back to the host part of the
     * address, because that is what the user typed and therefore recognises.
     */
    fun label(pairing: Pairing): String {
        val url: HttpUrl? = dev.perch.console.core.herdrclient.BridgeClient.parseAddress(pairing.address)
        return url?.host ?: pairing.address
    }

    private fun plainPrefs() = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    private companion object {
        const val PREFS = "perch.pairing"
        const val KEY_ADDRESS = "address"
        const val KEY_TOKEN = "token"
    }
}
