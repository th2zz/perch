package dev.perch.console.features.onboarding

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.auth.PairInvite

/**
 * One tap to pair, because the link already carried everything.
 *
 * The screen exists rather than connecting silently, and that is the whole
 * design: the user asked to connect to a computer, and this is the moment they
 * find out *which* one and grant it access to their agents. Skipping it would
 * save one tap and spend a security decision the user never made.
 *
 * There is no address field and no token field. If the user would rather type,
 * that is the other screen — one tap away, not the default.
 */
@Composable
fun InviteScreen(
    invite: PairInvite,
    busy: Boolean,
    error: String?,
    onAccept: () -> Unit,
    onUseManualEntry: () -> Unit,
) {
    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Text(
            text = stringResource(R.string.invite_title),
            style = MaterialTheme.typography.headlineSmall,
        )
        Text(
            text = invite.computerName.ifBlank { invite.address },
            style = MaterialTheme.typography.titleMedium,
        )
        Text(
            text = invite.address,
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = stringResource(R.string.invite_body),
            style = MaterialTheme.typography.bodyMedium,
        )

        if (error != null) {
            Text(
                text = error,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodyMedium,
            )
        }

        Button(
            onClick = onAccept,
            enabled = !busy,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text(stringResource(R.string.invite_action))
        }

        TextButton(onClick = onUseManualEntry, enabled = !busy) {
            Text(stringResource(R.string.invite_manual))
        }

        // No second "nothing changes on your computer" line here: the paragraph
        // above already says it, and this screen has six elements. On the manual
        // screen the footer earns its place because nothing else says it.
    }
}
