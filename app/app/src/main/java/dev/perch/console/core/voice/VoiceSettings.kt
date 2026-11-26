package dev.perch.console.core.voice

import android.content.Context
import androidx.core.content.edit
import dev.perch.console.core.auth.SecureStore

/**
 * The user's own transcription key, if they have one.
 *
 * Two decisions are baked into this small type, both from spec FR-014:
 *
 *  1. Voice is **optional**, and its absence is not an error. With no key
 *     configured the entry does not appear at all — a disabled microphone with a
 *     "configure me" tooltip would be an advertisement for work the user did not
 *     ask to do.
 *  2. The key is stored the same way the pairing token is: encrypted with a key
 *     that lives in the platform keystore and never leaves it. It is the user's
 *     own credential for a service we do not operate, so losing it would be worse
 *     than losing ours.
 *
 * The audio itself never reaches anything of ours. There is no server in this path
 * by construction: the app posts the recording straight to the provider the user
 * chose, with the key the user supplied. The relay's payload whitelist has no
 * field that could carry audio, which is a second, independent reason it cannot
 * leak.
 */
data class VoiceConfig(
    /** Base URL of an OpenAI-compatible transcription endpoint. */
    val endpoint: String,
    val apiKey: String,
    val model: String,
) {
    val configured: Boolean get() = apiKey.isNotBlank() && endpoint.isNotBlank()
}

class VoiceSettings(private val context: Context) {

    fun load(): VoiceConfig? {
        val key = SecureStore.get(context, KEY_API) ?: return null
        if (key.isBlank()) return null
        val prefs = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
        return VoiceConfig(
            endpoint = prefs.getString(KEY_ENDPOINT, null) ?: DEFAULT_ENDPOINT,
            apiKey = key,
            model = prefs.getString(KEY_MODEL, null) ?: DEFAULT_MODEL,
        ).takeIf { it.configured }
    }

    fun save(config: VoiceConfig) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit {
            putString(KEY_ENDPOINT, config.endpoint.trim())
            putString(KEY_MODEL, config.model.trim())
        }
        SecureStore.put(context, KEY_API, config.apiKey.trim())
    }

    fun clear() {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit {
            remove(KEY_ENDPOINT)
            remove(KEY_MODEL)
        }
        SecureStore.remove(context, KEY_API)
    }

    companion object {
        private const val PREFS = "perch.voice"
        private const val KEY_ENDPOINT = "endpoint"
        private const val KEY_MODEL = "model"
        private const val KEY_API = "voice.api_key"

        /**
         * The default is OpenAI's transcription endpoint.
         *
         * Only the shape is assumed, not the brand: this is the request format the
         * `/v1/audio/transcriptions` convention uses, and the field is editable so a
         * user whose provider speaks the same shape can point it elsewhere.
         */
        const val DEFAULT_ENDPOINT = "https://api.openai.com/v1/audio/transcriptions"
        const val DEFAULT_MODEL = "whisper-1"
    }
}
