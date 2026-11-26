package dev.perch.console.features.onboarding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.text.KeyboardOptions
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
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight

/**
 * The pairing screen.
 *
 * Two fields is the whole ceremony, and they are two fields rather than one
 * because collapsing them into a single "paste this" string would make a mistake
 * impossible to correct: the address and the token fail differently, and the user
 * needs to see which one is wrong.
 *
 * The screen also states, unprompted, that nothing on the computer was changed.
 * That sentence is doing real work — it is the answer to the objection this
 * audience is most likely to have (spec US1).
 */
@Composable
fun OnboardingScreen(
    busy: Boolean,
    error: String?,
    onPair: (address: String, token: String) -> Unit,
) {
    var address by remember { mutableStateOf("") }
    var token by remember { mutableStateOf("") }
    // Folded away by default. This screen's job is to get the user to the link;
    // showing the form open made the fallback look like the instruction, and left
    // a disabled button as the most prominent thing on the very first screen
    // anyone sees.
    var typingByHand by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = stringResource(R.string.pair_title),
            style = MaterialTheme.typography.headlineSmall,
        )
        Text(
            text = stringResource(R.string.pair_step_one),
            style = MaterialTheme.typography.bodyMedium,
        )
        Text(
            text = stringResource(R.string.pair_command),
            style = MaterialTheme.typography.bodyMedium,
            // A command is read character by character, so it gets the readable
            // font rather than whatever the body text uses.
            fontFamily = FontFamily.Monospace,
            fontWeight = FontWeight.Medium,
        )
        Text(
            text = stringResource(R.string.pair_step_two),
            style = MaterialTheme.typography.bodyMedium,
        )
        Spacer(Modifier.height(8.dp))

        // The fallback, deliberately folded away: opening it is a decision the user
        // makes after reading that the link is easier.
        if (!typingByHand) {
            TextButton(onClick = { typingByHand = true }) {
                Text(stringResource(R.string.pair_manual_heading))
            }
        } else {
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.pair_manual_heading),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Text(
                text = stringResource(R.string.pair_fields_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            OutlinedTextField(
                value = address,
                onValueChange = { address = it },
                label = { Text(stringResource(R.string.pair_address)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(
                    // A host and port is not prose: the URL keyboard gives the user
                    // the digits and the colon without a layout switch.
                    keyboardType = KeyboardType.Uri,
                    imeAction = ImeAction.Next,
                ),
                modifier = Modifier.fillMaxWidth(),
            )

            OutlinedTextField(
                value = token,
                onValueChange = { token = it },
                label = { Text(stringResource(R.string.pair_token)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                modifier = Modifier.fillMaxWidth(),
            )

            if (error != null) {
                Text(
                    text = error,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodyMedium,
                )
            }

            Button(
                onClick = { onPair(address, token) },
                enabled = !busy && address.isNotBlank() && token.isNotBlank(),
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.pair_action))
            }

            Spacer(Modifier.height(8.dp))
            Text(
                text = stringResource(R.string.pair_paste_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )        }

        Text(
            text = stringResource(R.string.pair_no_config_needed),
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
