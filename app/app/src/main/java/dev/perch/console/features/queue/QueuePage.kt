package dev.perch.console.features.queue

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.format.Time
import dev.perch.console.core.format.Markdown
import dev.perch.console.core.format.NotificationText
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.herdrclient.Host

/**
 * The attention queue: one card per thing that is waiting for a person.
 *
 * A card carries exactly enough to decide without opening anything else — who,
 * which project, and what they are asking — plus one obvious action. The excerpt
 * collapses by default because a card that shows four lines of terminal output
 * is a card the user scrolls past; expanding it is one tap and it is the tap
 * they already want to make.
 */
@Composable
fun QueuePage(
    waiting: List<Decision>,
    busyDecisionId: String?,
    onDecide: (Decision, DecisionKind) -> Unit,
    onAnswer: (Decision) -> Unit,
    onOpenSession: (Decision) -> Unit,
    modifier: Modifier = Modifier,
) {
    if (waiting.isEmpty()) {
        Column(Modifier.padding(24.dp)) {
            Text(
                text = stringResource(R.string.nothing_waiting),
                style = MaterialTheme.typography.bodyLarge,
            )
        }
        return
    }

    LazyColumn(
        modifier = modifier,
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        items(waiting, key = { it.id }) { decision ->
            DecisionCard(
                decision = decision,
                busy = busyDecisionId == decision.id,
                onDecide = { kind -> onDecide(decision, kind) },
                onAnswer = { onAnswer(decision) },
                onOpenSession = { onOpenSession(decision) },
            )
        }
    }
}

@Composable
private fun DecisionCard(
    decision: Decision,
    busy: Boolean,
    onDecide: (DecisionKind) -> Unit,
    onAnswer: () -> Unit,
    onOpenSession: () -> Unit,
) {
    var expanded by remember { mutableStateOf(false) }
    val detail = NotificationText.excerpt(decision.promptText)

    Card(
        modifier = Modifier.fillMaxWidth(),
        colors = CardDefaults.cardColors(),
    ) {
        Column(
            modifier = Modifier.padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Row(modifier = Modifier.fillMaxWidth()) {
                Column(Modifier.weight(1f)) {
                    Text(
                        // The computer is named only when the request did not come
                        // from this one: with a single machine the name is noise,
                        // and with two it is the difference between two cards that
                        // otherwise read identically (spec FR-016).
                        text = if (decision.hostId.isNotBlank() && decision.hostId != Host.LOCAL) {
                            stringResource(
                                R.string.decision_title_on_host,
                                decision.agentLabel,
                                decision.projectLabel,
                                decision.hostId,
                            )
                        } else {
                            stringResource(
                                R.string.decision_title,
                                decision.agentLabel,
                                decision.projectLabel,
                            )
                        },
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Medium,
                    )
                    val ago = Time.ago(decision.createdAt)
                    if (ago.isNotEmpty()) {
                        Text(
                            text = ago,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }

            if (detail.isNotEmpty()) {
                // The headline is what a collapsed card shows, and the toggle is only
                // worth its space when opening it reveals something more. On a card
                // whose whole question is one line — "Do you want to proceed?" — the
                // link expanded to the same sentence, which reads as a button that
                // does nothing. Found by looking at the rendered queue, not in code.
                val headline = NotificationText.headline(decision.promptText)
                val revealsMore = detail.trim() != headline.trim()
                if (expanded) {
                    Text(
                        text = detail,
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                    )
                } else if (headline.isNotBlank()) {
                    // The question, not the last option. A collapsed card that reads
                    // "3. No" tells the user nothing about what is being asked. One
                    // line is a hint, so the markers come off and nothing else: a `**`
                    // on a card the user is deciding from is punctuation with no
                    // purpose.
                    Text(
                        text = Markdown.plain(headline),
                        style = MaterialTheme.typography.bodySmall,
                        fontFamily = FontFamily.Monospace,
                        maxLines = 2,
                    )
                }
                if (revealsMore) {
                    TextButton(onClick = { expanded = !expanded }) {
                        Text(stringResource(R.string.show_what_it_asked))
                    }
                }
            }

            DecisionActions(
                decision = decision,
                busy = busy,
                onDecide = onDecide,
                onAnswer = onAnswer,
                onOpenSession = onOpenSession,
            )

            AnimatedVisibility(visible = busy) {
                Text(
                    text = "…",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
