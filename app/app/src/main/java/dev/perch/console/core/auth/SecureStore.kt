package dev.perch.console.core.auth

import android.content.Context
import androidx.core.content.edit
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Stores the one secret the app holds: the pairing token.
 *
 * The token identifies this phone to the user's computer, so a plain
 * SharedPreference string would be readable by anything that can read the app's
 * data directory — including a backup. Instead the value is encrypted with an
 * AES key that lives in the Android keystore and never leaves it, and the
 * keystore key is generated with `setUserAuthenticationRequired(false)` because
 * the app must be able to check for new requests without the user unlocking a
 * second time.
 *
 * This is deliberately not a third-party crypto dependency: the platform
 * keystore is the thing that actually provides the guarantee, and a library on
 * top of it would only add a supply-chain surface.
 */
object SecureStore {
    private const val KEYSTORE = "AndroidKeyStore"
    private const val KEY_ALIAS = "perch.pairing.v1"
    private const val PREFS = "perch.secure"
    private const val GCM_TAG_BITS = 128
    private const val IV_BYTES = 12

    fun put(context: Context, name: String, value: String) {
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(Cipher.ENCRYPT_MODE, secretKey())
        val ciphertext = cipher.doFinal(value.toByteArray(Charsets.UTF_8))
        val packed = Base64.encodeToString(cipher.iv + ciphertext, Base64.NO_WRAP)
        prefs(context).edit { putString(name, packed) }
    }

    fun get(context: Context, name: String): String? {
        val packed = prefs(context).getString(name, null) ?: return null
        return runCatching {
            val raw = Base64.decode(packed, Base64.NO_WRAP)
            if (raw.size <= IV_BYTES) return null
            val iv = raw.copyOfRange(0, IV_BYTES)
            val body = raw.copyOfRange(IV_BYTES, raw.size)
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            cipher.init(Cipher.DECRYPT_MODE, secretKey(), GCMParameterSpec(GCM_TAG_BITS, iv))
            String(cipher.doFinal(body), Charsets.UTF_8)
        }.getOrNull()
    }

    fun remove(context: Context, name: String) {
        prefs(context).edit { remove(name) }
    }

    private fun prefs(context: Context) =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    private fun secretKey(): SecretKey {
        val keyStore = KeyStore.getInstance(KEYSTORE).apply { load(null) }
        (keyStore.getEntry(KEY_ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }

        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                KEY_ALIAS,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .setKeySize(256)
                .setUserAuthenticationRequired(false)
                .build(),
        )
        return generator.generateKey()
    }
}
