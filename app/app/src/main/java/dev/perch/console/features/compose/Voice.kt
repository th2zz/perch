package dev.perch.console.features.compose

import android.content.Context
import android.media.MediaRecorder
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import java.io.File

/**
 * Records a voice note to a file.
 *
 * Deliberately the whole of the recording implementation: one file, started and
 * stopped, deleted by the caller in every case including failure. A recording is a
 * few seconds of the user's voice, so the correct amount of it to keep is none.
 *
 * The file is handed straight to the transcriber and to nothing else. There is no
 * copy in the app's storage, no upload anywhere else, and no queue — which is how
 * spec FR-014's "audio does not pass through our servers" is a property of the code
 * rather than a promise in a policy.
 */
class VoiceRecorder(private val context: Context) {

    private var recorder: MediaRecorder? = null
    private var target: File? = null

    val recording: Boolean get() = recorder != null

    /** Starts recording. Returns the file being written, or null if it could not start. */
    fun start(): File? {
        if (recorder != null) return target
        val file = File.createTempFile("perch-voice-", ".m4a", context.cacheDir)
        return try {
            @Suppress("DEPRECATION")
            val created = MediaRecorder(context)
            created.setAudioSource(MediaRecorder.AudioSource.MIC)
            created.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
            created.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            created.setAudioEncodingBitRate(96_000)
            created.setAudioSamplingRate(44_100)
            created.setOutputFile(file.absolutePath)
            created.prepare()
            created.start()
            recorder = created
            target = file
            file
        } catch (e: Exception) {
            // The usual cause is the microphone permission having been refused, and
            // it is not worth distinguishing: either way there is no recording, and
            // the caller says so in the user's terms.
            runCatching { recorder?.release() }
            recorder = null
            file.delete()
            target = null
            null
        }
    }

    /** Stops and returns the recording, or null if nothing usable was captured. */
    fun stop(): File? {
        val created = recorder ?: return null
        val file = target
        recorder = null
        target = null
        return try {
            created.stop()
            created.release()
            file?.takeIf { it.exists() && it.length() > 0 }
        } catch (e: Exception) {
            // `stop()` throws when it was stopped too soon to have written anything.
            runCatching { created.release() }
            file?.delete()
            null
        }
    }

    /** Throws the recording away without transcribing it. */
    fun cancel() {
        val created = recorder
        val file = target
        recorder = null
        target = null
        runCatching { created?.stop() }
        runCatching { created?.release() }
        file?.delete()
    }
}

/**
 * The microphone row, shown only when the user has set up a key of their own.
 *
 * Spec FR-014 is explicit that the entry must be absent rather than disabled when
 * voice is not configured, and that is also the better product: a greyed-out
 * microphone with a tooltip is an advertisement for work the user did not ask to do.
 *
 * Nothing here sends anything. The transcript lands in the text box, and the user
 * still has to press Send — speaking is a faster way to write, not a faster way to
 * act. That distinction is what keeps a misheard word from being submitted.
 */
@Composable
fun MicRow(
    recording: Boolean,
    transcribing: Boolean,
    canStart: Boolean,
    onStart: () -> Unit,
    onStop: () -> Unit,
    onDiscard: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        when {
            transcribing -> {
                CircularProgressIndicator()
                Text(
                    text = stringResource(R.string.voice_transcribing),
                    style = MaterialTheme.typography.bodySmall,
                )
            }
            recording -> {
                Text(
                    text = stringResource(R.string.voice_listening),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                )
                TextButton(onClick = onDiscard) { Text(stringResource(R.string.voice_discard)) }
                TextButton(onClick = onStop) { Text(stringResource(R.string.voice_stop)) }
            }
            else -> {
                TextButton(onClick = onStart, enabled = canStart) {
                    Text(stringResource(R.string.voice_start))
                }
                Text(
                    text = stringResource(R.string.voice_hint),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
