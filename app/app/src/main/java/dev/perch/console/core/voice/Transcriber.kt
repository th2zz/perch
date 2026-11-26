package dev.perch.console.core.voice

import java.io.File
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.MultipartBody
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.asRequestBody

/** What came back, or why it did not. */
sealed interface Transcription {
    data class Text(val text: String) : Transcription

    /**
     * A failure worth showing the user.
     *
     * `fix` is not optional in spirit: a transcription failure the user cannot act
     * on is worse than no microphone at all, because they have already spoken.
     */
    data class Failed(val reason: String, val fix: String) : Transcription
}

/**
 * Sends a recording to the provider the user chose, with the key the user supplied.
 *
 * This is the whole of the audio path. There is deliberately no queue, no retry
 * store and no fallback: a recording is a few seconds of the user's voice, and
 * keeping it around after a failure would be the opposite of the promise FR-014
 * makes. The caller deletes the file whatever happens.
 *
 * The client is separate from the bridge client on purpose, and its timeouts say
 * why: every call to the user's own computer is bounded by the ten seconds a
 * notification action gets, but transcription is a foreground action on a file the
 * user just recorded, and providers take their time. Sharing one budget between
 * the two would either cut transcriptions off or let a broken bridge hang a screen.
 */
class Transcriber(
    private val client: OkHttpClient = defaultClient(),
    private val json: Json = Json { ignoreUnknownKeys = true },
) {

    suspend fun transcribe(audio: File, config: VoiceConfig): Transcription {
        if (!config.configured) {
            return Transcription.Failed(
                reason = "No transcription key is set up.",
                fix = "Add one in Settings, or type your instruction instead.",
            )
        }
        if (!audio.exists() || audio.length() == 0L) {
            return Transcription.Failed(
                reason = "Nothing was recorded.",
                fix = "Check that Perch is allowed to use the microphone, then try again.",
            )
        }

        val body = MultipartBody.Builder()
            .setType(MultipartBody.FORM)
            .addFormDataPart("model", config.model)
            .addFormDataPart(
                "file",
                audio.name,
                audio.asRequestBody("audio/mp4".toMediaType()),
            )
            .build()

        val request = Request.Builder()
            .url(config.endpoint)
            .header("Authorization", "Bearer ${config.apiKey}")
            .post(body)
            .build()

        return withContext(Dispatchers.IO) {
            try {
                client.newCall(request).execute().use { response ->
                    val text = response.body.string()
                    if (!response.isSuccessful) {
                        return@use failure(response.code, text)
                    }
                    val spoken = readText(text)
                    if (spoken.isNullOrBlank()) {
                        Transcription.Failed(
                            reason = "Nothing came back from the transcriber.",
                            fix = "Try again, speaking a little closer to the phone.",
                        )
                    } else {
                        Transcription.Text(spoken)
                    }
                }
            } catch (e: Exception) {
                Transcription.Failed(
                    reason = e.message ?: "Could not reach the transcriber.",
                    fix = "Check your connection, then try again.",
                )
            }
        }
    }

    /** Reads the `text` field, whatever else the provider wraps it in. */
    private fun readText(body: String): String? =
        runCatching {
            (json.parseToJsonElement(body) as? JsonObject)
                ?.get("text")
                ?.jsonPrimitive
                ?.content
        }.getOrNull()

    /**
     * Turns an HTTP failure into something the user can act on.
     *
     * The two that matter are the two that are actually actionable: a rejected key
     * (which the user can replace) and a rate limit (which passes). Everything else
     * gets the honest version — it did not work, here is what to do instead.
     */
    private fun failure(status: Int, body: String): Transcription.Failed = when (status) {
        401, 403 -> Transcription.Failed(
            reason = "Your transcription key was refused.",
            fix = "Check the key in Settings, or type your instruction instead.",
        )
        429 -> Transcription.Failed(
            reason = "The transcriber is rate limiting you.",
            fix = "Wait a moment and try again.",
        )
        else -> Transcription.Failed(
            reason = "The transcriber returned $status.",
            fix = "Check the endpoint in Settings, or type your instruction instead.",
        )
    }

    companion object {
        fun defaultClient(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(10, TimeUnit.SECONDS)
            // Uploading a recording and waiting for a transcript is a foreground
            // action, not a call whose budget is someone else's window.
            .writeTimeout(30, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .callTimeout(90, TimeUnit.SECONDS)
            .build()
    }
}
