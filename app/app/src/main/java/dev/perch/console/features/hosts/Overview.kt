package dev.perch.console.features.hosts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.herdrclient.AgentStates
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.core.herdrclient.WireValues

/**
 * The portrait overview: every project and its agents, on one screen, with no
 * horizontal scrolling (spec FR-007, SC-010).
 *
 * The layout is a single scrolling column because that is what a phone is good
 * at. Nothing here is interactive beyond tapping a project, and tapping only
 * changes which project the *phone* is looking at — it never moves focus on the
 * computer (spec FR-008, and contracts/herdr-usage.md §3, which forbids
 * pane.focus).
 */
@Composable
fun Overview(
    mirror: Mirror,
    onOpenWorkspace: (Workspace) -> Unit,
    onNewProject: () -> Unit = {},
) {
    LazyColumn(
        contentPadding = PaddingValues(16.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item(key = "new-project") {
            TextButton(onClick = onNewProject) {
                Text(stringResource(R.string.new_project))
            }
        }
        // Grouped by computer, and only when there is more than one to tell apart.
        // A single-machine user sees exactly what they saw before; the moment a
        // second machine answers, every project says which one it is on (spec
        // FR-016) and the computers that could not be read say why (FR-015).
        for (hostId in mirror.hostIdsWithContent()) {
            val host = mirror.hosts.firstOrNull { Mirror.sameHost(it.hostId, hostId) }
            val workspaces = mirror.workspaces
                .filter { Mirror.sameHost(it.hostId, hostId) }
                .sortedBy { it.number }
            if (mirror.showHostNames && workspaces.isNotEmpty()) {
                item(key = "host-$hostId") {
                    Text(
                        text = mirror.hostLabel(hostId),
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
            items(workspaces, key = { "${Mirror.normalizeHost(it.hostId)}/${it.workspaceId}" }) { workspace ->
                WorkspaceCard(
                    workspace = workspace,
                    agents = mirror.panesOf(workspace.hostId, workspace.workspaceId)
                        .filter { it.agent != null }
                        // The parentheses matter: `to` binds tighter than `?:`, so
                        // without them the elvis chain would build a Pair on only
                        // one branch.
                        .map { (it.displayAgent ?: it.agent ?: "agent") to it.agentStatus },
                    onClick = { onOpenWorkspace(workspace) },
                )
            }
            if (host != null && !Mirror.sameHost(host.hostId, Host.LOCAL) && host.status == WireValues.HostStatus.UNREACHABLE) {
                item(key = "unreachable-$hostId") {
                    // A computer that did not answer is stated rather than hidden:
                    // otherwise its projects look like projects that vanished.
                    UnreachableHostRow(host = host, showName = mirror.showHostNames)
                }
            }
        }
    }
}

@Composable
private fun UnreachableHostRow(host: Host, showName: Boolean) {
    Card(modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Text(
                text = if (showName) host.label else stringResource(R.string.host_unreachable_title),
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.error,
            )
            Text(
                text = host.reason.ifBlank { stringResource(R.string.host_unreachable_title) },
                style = MaterialTheme.typography.bodySmall,
            )
            if (host.fix.isNotBlank()) {
                Text(
                    text = host.fix,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun WorkspaceCard(
    workspace: Workspace,
    agents: List<Pair<String, String>>,
    onClick: () -> Unit,
) {
    Card(modifier = Modifier.fillMaxWidth(), onClick = onClick) {
        Column(Modifier.padding(16.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
            ) {
                Text(
                    text = workspace.label.ifBlank { "project ${workspace.number}" },
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Medium,
                )
                // The status only earns its place here when the project has more
                // than one agent: then it is the summary, and the lines below are
                // the detail. With a single agent the line below already says it,
                // and the rendered card showed the same word twice — which reads
                // as a bug rather than as emphasis.
                if (agents.size > 1) {
                    Text(
                        text = statusLabel(workspace.agentStatus),
                        style = MaterialTheme.typography.labelMedium,
                        color = statusColour(workspace.agentStatus),
                    )
                }
            }
            if (agents.isEmpty()) {
                Text(
                    text = stringResource(R.string.no_agent_running),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                agents.forEach { (name, status) ->
                    Text(
                        text = "$name · ${statusLabel(status)}",
                        style = MaterialTheme.typography.bodyMedium,
                    )
                }
            }
        }
    }
}

/**
 * The four states the user sees, in words rather than in herdr's vocabulary.
 *
 * "unknown" is shown as its own thing and never folded into "finished": the
 * spec's edge case forbids presenting an undecidable state as completion, and
 * saying "unknown" is the honest option.
 */
@Composable
fun statusLabel(status: String): String = when (status) {
    // These come from resources rather than from this file. They are the words the
    // user reads most often, and a literal here is a sentence that can never be
    // translated — which is how a product ends up English-only by accident.
    AgentStates.BLOCKED -> stringResource(R.string.status_blocked)
    AgentStates.WORKING -> stringResource(R.string.status_working)
    AgentStates.IDLE -> stringResource(R.string.status_idle)
    AgentStates.DONE -> stringResource(R.string.status_done)
    else -> stringResource(R.string.status_unknown)
}

@Composable
private fun statusColour(status: String) = when (status) {
    AgentStates.BLOCKED -> MaterialTheme.colorScheme.error
    AgentStates.WORKING -> MaterialTheme.colorScheme.primary
    else -> MaterialTheme.colorScheme.onSurfaceVariant
}
