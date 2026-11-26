package dev.perch.console.features.insight

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
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
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.format.Duration
import dev.perch.console.core.herdrclient.ValueStats

/**
 * What the week added up to.
 *
 * Two rules govern this card, and both come from the spec rather than from taste
 * (FR-013):
 *
 *  1. Every number is attributable. "8 minutes" is only meaningful next to the
 *     assumption behind it, so the baseline is on the card, behind one tap —
 *     visible, but not shouting at someone who already knows.
 *  2. There is no black box. The second sentence explains exactly how the figure
 *     was produced, including that it is an estimate.
 *
 * The card is also the honest place for a zero: a first-week user with no
 * decisions yet should see that plainly, not a fake streak.
 */
@Composable
fun WeekSummary(stats: ValueStats, modifier: Modifier = Modifier) {
    var expanded by remember { mutableStateOf(false) }

    Card(
        modifier = modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant,
        ),
    ) {
        Column(Modifier.padding(12.dp)) {
            if (stats.decisions == 0) {
                Text(
                    text = stringResource(R.string.week_nothing_yet),
                    style = MaterialTheme.typography.bodyMedium,
                )
                Text(
                    text = stringResource(R.string.week_nothing_yet_detail),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                return@Column
            }

            Text(
                text = stringResource(
                    R.string.week_headline,
                    Duration.humanise(stats.secondsSaved),
                ),
                style = MaterialTheme.typography.titleSmall,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(4.dp)) {
                Text(
                    // A plural, so that one decision reads as "1 decision" rather
                    // than "1 decisions". The count decides the wording, which is
                    // what makes a translated sentence read as a sentence.
                    text = pluralStringResource(
                        R.plurals.week_counts,
                        stats.decisions,
                        stats.decisions,
                        stats.notAtComputer,
                    ),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            TextButton(onClick = { expanded = !expanded }) {
                Text(stringResource(R.string.week_how))
            }
            AnimatedVisibility(visible = expanded) {
                Text(
                    text = pluralStringResource(
                        R.plurals.week_baseline,
                        stats.baselineSeconds,
                        stats.baselineSeconds,
                    ),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}
