package dev.perch.console.features.queue

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.ui.platform.LocalContext
import androidx.core.content.ContextCompat
import android.Manifest
import android.content.pm.PackageManager
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import dev.perch.console.R
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.features.compose.MicRow

/**
 * The reply box — a plain platform text field, on purpose.
 *
 * This is the single most important implementation choice in the app. The text
 * the user types never goes near a terminal emulator: it is a normal Android
 * `TextField`, so the system IME owns composition, candidate selection,
 * autocorrect and paste exactly as it does in any other app. Chinese, Japanese
 * and Korean input therefore work because nothing in this path can break them.
 *
 * The bytes are delivered in one step, on Send, through the bridge's
 * `agent.prompt` — which is also what makes "discard" meaningful: until Send is
 * pressed, the computer has received nothing at all (spec US3 scenario 3).
 */
@Composable
fun ComposeReply(
    decision: Decision,
    busy: Boolean,
    voiceConfigured: Boolean,
    voiceRecording: Boolean,
    voiceTranscribing: Boolean,
    transcript: String?,
    onVoiceStart: () -> Unit,
    onVoiceStop: () -> Unit,
    onVoiceDiscard: () -> Unit,
    onSend: (String) -> Unit,
    onDismiss: () -> Unit,
) {
    var text by remember { mutableStateOf("") }
    val context = LocalContext.current

    // Asked for at the moment it is needed, which is the only moment the request
    // makes sense to the person reading it: they just tapped a microphone.
    val microphonePermission = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted -> if (granted) onVoiceStart() }

    // A transcript arrives asynchronously, so it is inserted when it appears rather
    // than passed as the initial value. It replaces whatever is in the box, which is
    // what "speak instead" means; the user can still edit before sending.
    LaunchedEffect(transcript) {
        if (!transcript.isNullOrBlank()) text = transcript
    }

    Dialog(
        onDismissRequest = onDismiss,
        // Full screen: the keyboard plus a multi-line box needs the room, and a
        // half-height dialog would leave the user editing three lines at a time.
        properties = DialogProperties(usePlatformDefaultWidth = false),
    ) {
        Surface(modifier = Modifier.fillMaxSize()) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(16.dp)
                    // Keeps the box above the keyboard instead of under it.
                    .imePadding(),
                verticalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Text(
                    text = stringResource(R.string.compose_title),
                    style = MaterialTheme.typography.titleMedium,
                )
                Text(
                    text = "${decision.agentLabel} · ${decision.projectLabel}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )

                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    modifier = Modifier
                        .fillMaxWidth()
                        .weight(1f),
                    placeholder = { Text(stringResource(R.string.compose_hint)) },
                    // Multi-line, but the send action is an explicit button rather
                    // than the keyboard's action key: a stray Enter must not send
                    // half a sentence, and agents treat newlines as meaningful.
                    singleLine = false,
                )

                if (voiceConfigured) {
                    MicRow(
                        recording = voiceRecording,
                        transcribing = voiceTranscribing,
                        canStart = !busy,
                        onStart = {
                            val granted = ContextCompat.checkSelfPermission(
                                context,
                                Manifest.permission.RECORD_AUDIO,
                            ) == PackageManager.PERMISSION_GRANTED
                            if (granted) onVoiceStart() else microphonePermission.launch(Manifest.permission.RECORD_AUDIO)
                        },
                        onStop = onVoiceStop,
                        onDiscard = onVoiceDiscard,
                    )
                }

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    TextButton(onClick = onDismiss, enabled = !busy) {
                        Text(stringResource(R.string.compose_discard))
                    }
                    TextButton(
                        onClick = { onSend(text) },
                        enabled = !busy && text.isNotBlank(),
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(stringResource(R.string.compose_send))
                    }
                }
            }
        }
    }
}
