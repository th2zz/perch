package dev.perch.console.features.terminal

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.format.Duration
import dev.perch.console.core.herdrclient.ConversationMessage
import dev.perch.console.core.herdrclient.Speakers
import dev.perch.console.core.herdrclient.Transcript
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.ui.Alignment

@Composable
fun ConversationFace(
    conversation: Transcript,
    modifier: Modifier = Modifier,
) {
    Column(modifier = modifier, verticalArrangement = Arrangement.spacedBy(12.dp)) {
        // Said only when it is true, and worth saying: a conversation that starts part way through
        // otherwise reads as the beginning of one.
        if (conversation.truncated) {
            Text(
                text = stringResource(R.string.session_truncated),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        if (conversation.isEmpty) {
            Text(
                text = stringResource(R.string.session_nothing_said),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }

        for (message in conversation.messages) {
            // An unknown speaker is drawn as a voice of the screen rather than as the user's own
            // words: showing a message under "You" that the user did not write is a claim about them,
            // and the one thing this face must never do is put words in their mouth.
            if (message.role == Speakers.YOU) YourTurn(message) else AgentTurn(message)
        }

        if (conversation.working) {
            WorkingRow(seconds = conversation.workingSecs)
        }
    }
}

/**
 * One of the user's own turns, marked as theirs.
 *
 * A bar down the side rather than a bubble: the same idiom the renderer uses for a quotation, so
 * "this came from somewhere else" already means something on this screen. It is not Markdown — this
 * is what the user typed, and turning `**this**` into bold is a reinterpretation of their words that
 * buys nothing.
 */
@Composable
private fun YourTurn(message: ConversationMessage) {
    Row(
        // IntrinsicSize.Min so the bar is as tall as the text beside it; a bar with no content of its
        // own measures zero.
        modifier = Modifier.fillMaxWidth().height(IntrinsicSize.Min),
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Box(
            modifier = Modifier
                .width(3.dp)
                .fillMaxHeight()
                .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.55f)),
        )
        Column {
            Text(
                text = stringResource(R.string.session_you),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.primary,
            )
            Text(
                text = message.text,
                style = MaterialTheme.typography.bodyMedium,
            )
        }
    }
}

/**
 * The agent's answer, rendered.
 *
 * No label: the agent is the voice of this screen, and its answers are the body of what is being read
 * — a "Agent" tag on every one of them would be a word repeated down the page for no information. The
 * user's turns are the ones that need marking, because they are the interruptions.
 */
@Composable
private fun AgentTurn(message: ConversationMessage) {
    MarkdownText(text = message.text, modifier = Modifier.fillMaxWidth())
}

/**
 * Working, and for how long.
 *
 * The duration is the computer's own measurement of the turn ([Transcript.workingSecs]) rather than
 * anything the phone works out: two clocks on two machines disagree, and a duration computed from both
 * can come out negative. Coarse ("2 minutes") rather than a ticking stopwatch, because it answers "is
 * this still going, or has it been a while?" — which is the question a person away from their desk
 * actually has.
 */
@Composable
private fun WorkingRow(seconds: Int) {
    Row(
        modifier = Modifier.fillMaxWidth().padding(top = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        CircularProgressIndicator(modifier = Modifier.width(14.dp).height(14.dp), strokeWidth = 2.dp)
        Text(
            text = if (seconds > 0) {
                stringResource(R.string.session_working, Duration.humanise(seconds))
            } else {
                // No measurement, no number: an agent whose records nobody has taught Perch to read
                // still works, and saying so beats inventing a stopwatch.
                stringResource(R.string.status_working)
            },
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
