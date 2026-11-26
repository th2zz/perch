package dev.perch.console.features.queue

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionKind

/**
 * The actions on one decision.
 *
 * This composable is where the product's central promise either holds or breaks:
 * three buttons at most, no screen change, no keyboard required (spec SC-002 and
 * SC-010 both land here).
 *
 * When the computer has told us it cannot translate the action into keys, the
 * buttons are not drawn at all. That is deliberate and it is the honest option:
 * a button that cannot work teaches the user that the app is unreliable, whereas
 * an explanation teaches them that this particular prompt needs the computer.
 */
@Composable
fun DecisionActions(
    decision: Decision,
    busy: Boolean,
    onDecide: (DecisionKind) -> Unit,
    onAnswer: () -> Unit,
    onOpenSession: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier.fillMaxWidth()) {
        if (decision.approvable || decision.rejectable) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                if (decision.approvable) {
                    Button(
                        onClick = { onDecide(DecisionKind.APPROVE) },
                        enabled = !busy,
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(stringResource(R.string.approve))
                    }
                }
                if (decision.rejectable) {
                    OutlinedButton(
                        onClick = { onDecide(DecisionKind.REJECT) },
                        enabled = !busy,
                        modifier = Modifier.weight(1f),
                    ) {
                        Text(stringResource(R.string.reject))
                    }
                }
                if (busy) {
                    CircularProgressIndicator(modifier = Modifier.padding(4.dp))
                }
            }
        }

        // Answering comes before the escape route, because answering is doing the
        // thing rather than going somewhere else to do it.
        if (decision.answerable) {
            TextButton(onClick = onAnswer, enabled = !busy) {
                Text(stringResource(R.string.answer))
            }
        }

        // The escape route is for the case where there is nothing else at all.
        //
        // This used to be gated on "no approve or reject keys", which produced a card
        // that said "Perch cannot press this one for you" directly above an Answer
        // button — a contradiction, and the kind that only a rendered screenshot
        // shows. `needsComputer` is the right question: it accounts for answering
        // too. Where something *can* be done, reading the session in full is still
        // available from the project screen, so the card stays within three actions.
        if (decision.needsComputer) {
            Column(Modifier.padding(top = 4.dp)) {
                Text(
                    text = stringResource(R.string.cannot_press_for_you),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                )
                decision.unmappableReason?.takeIf { it.isNotBlank() }?.let { reason ->
                    Text(text = reason, style = MaterialTheme.typography.bodySmall)
                }
                // No "go to your computer" line any more. The reason says what to do
                // (the escape hatch), and this button does it; telling the user to
                // walk to the computer *and* offering to open the session here was
                // two instructions pointing in opposite directions.
                //
                // The refusal still has to lead somewhere, or "we do not guess"
                // reads as "you are stuck". This is where it leads.
                TextButton(onClick = onOpenSession, enabled = !busy) {
                    Text(stringResource(R.string.open_the_session))
                }
            }
        }
    }
}
