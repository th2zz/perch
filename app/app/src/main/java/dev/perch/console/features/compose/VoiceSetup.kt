package dev.perch.console.features.compose

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.voice.VoiceSettings

/**
 * Where the user puts in their own transcription key.
 *
 * Folded away by default and framed as optional, because it is: the product works
 * without it, and the only reason to open this is that typing a long instruction on
 * a phone is unpleasant. Selling a feature that costs the user money is the kind of
 * thing that should be offered once, plainly, and then left alone.
 *
 * The key goes into the platform keystore, not into this screen's state beyond the
 * moment it is saved, and the body says where the recording goes — to the service
 * they chose, never through anything of ours.
 */
@Composable
fun VoiceSetup(
    configured: Boolean,
    onSave: (apiKey: String, endpoint: String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var expanded by remember { mutableStateOf(false) }
    var key by remember { mutableStateOf("") }
    var endpoint by remember { mutableStateOf(VoiceSettings.DEFAULT_ENDPOINT) }

    Column(modifier = modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 4.dp)) {
        TextButton(onClick = { expanded = !expanded }) {
            Text(
                text = if (configured) {
                    stringResource(R.string.voice_setup_title) + " · on"
                } else {
                    stringResource(R.string.voice_setup_title) + " · off"
                },
            )
        }

        AnimatedVisibility(visible = expanded) {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    text = stringResource(R.string.voice_setup_body),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = key,
                    onValueChange = { key = it },
                    label = { Text(stringResource(R.string.voice_setup_key)) },
                    singleLine = true,
                    // Nothing looks at this field, so it is rendered as a secret to
                    // keep it off a shoulder-surfer's screen and out of screenshots.
                    visualTransformation = PasswordVisualTransformation(),
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Password,
                        imeAction = ImeAction.Done,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = endpoint,
                    onValueChange = { endpoint = it },
                    label = { Text(stringResource(R.string.voice_setup_endpoint)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(
                        keyboardType = KeyboardType.Uri,
                        imeAction = ImeAction.Done,
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(
                        onClick = {
                            onSave(key, endpoint)
                            key = ""
                        },
                    ) {
                        Text(stringResource(R.string.voice_setup_save))
                    }
                    if (configured) {
                        TextButton(onClick = { onSave("", endpoint) }) {
                            Text(stringResource(R.string.voice_setup_remove))
                        }
                    }
                }
            }
        }
    }
}
