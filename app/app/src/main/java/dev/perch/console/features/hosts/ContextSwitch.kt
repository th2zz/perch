package dev.perch.console.features.hosts

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.AgentStates
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.features.manage.CloseProjectDialog
import dev.perch.console.features.manage.CloseSessionDialog
import dev.perch.console.features.manage.RenameDialog

/**
 * Tapping a project changes what *this phone* is looking at.
 *
 * The invariant here is the whole reason this file is separate from the others:
 * switching context on the phone must never touch the computer. It is the kind of
 * thing that would be easy to "improve" later by calling `pane.focus` so the
 * terminal follows along — which would be a fifth kind of write, would move the
 * user's cursor out from under them, and would break the promise that Perch only
 * ever answers requests.
 *
 * It is enforced rather than documented: the app can only express the four write
 * kinds, and the bridge rejects anything else, which is tested. So the most this
 * code can do is reorder what is on the phone's own screen.
 */
@Composable
fun WorkspaceDetail(
    workspace: Workspace,
    mirror: Mirror,
    onBack: () -> Unit,
    /** Opens the escape hatch: pane id, and the computer it is on. */
    onOpenSession: (String, String) -> Unit,
    onNewSession: () -> Unit = {},
    /** Names this project. */
    onRename: (String) -> Unit = {},
    /** Closes this project and everything in it; the confirmation has already happened on this side. */
    onEndProject: () -> Unit = {},
    onEndSession: (String, String) -> Unit = { _, _ -> },
    modifier: Modifier = Modifier,
) {
    // Scoped by computer as well as by workspace: every machine numbers its
    // workspaces from w1, so the id alone would pull in another computer's panes.
    val panes = mirror.panesOf(workspace.hostId, workspace.workspaceId)
    // Which of the two dialogs is open. Held here rather than in the console, because this is the screen the
    // question is asked from — and not remembered across a rotation, for the same reason the session's
    // confirmation is not: a dialog nobody is looking at is not consent.
    var renaming by remember { mutableStateOf(false) }
    var ending by remember { mutableStateOf(false) }
    var menuOpen by remember { mutableStateOf(false) }
    var endingPaneId by remember(workspace.hostId, workspace.workspaceId) { mutableStateOf<String?>(null) }

    Column(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 16.dp, vertical = 4.dp),
        ) {
            TextButton(onClick = onBack) { Text(stringResource(R.string.back_to_projects)) }
            Text(
                text = if (mirror.showHostNames) {
                    "${workspace.label.ifBlank { "project ${workspace.number}" }} · " +
                        mirror.hostLabel(workspace.hostId)
                } else {
                    workspace.label.ifBlank { "project ${workspace.number}" }
                },
                style = MaterialTheme.typography.titleSmall,
                modifier = Modifier
                    .weight(1f)
                    .padding(top = 12.dp),
            )
            // Naming and closing a project from the phone: the two things this product learned to do on
            // 2026-10-15. Kept behind a ⋮ in the header because a project screen is mostly a list of its
            // sessions, and neither of these is what somebody opens it for.
            Box {
                TextButton(onClick = { menuOpen = true }) {
                    Text(
                        text = "⋮",
                        style = MaterialTheme.typography.titleLarge,
                        modifier = Modifier.semantics { contentDescription = "Project menu" },
                    )
                }
                DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.new_session)) },
                        onClick = {
                            menuOpen = false
                            onNewSession()
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.manage_rename)) },
                        onClick = {
                            menuOpen = false
                            renaming = true
                        },
                    )
                    DropdownMenuItem(
                        text = { Text(stringResource(R.string.manage_close_project)) },
                        onClick = {
                            menuOpen = false
                            ending = true
                        },
                    )
                }
            }
        }

        if (renaming) {
            RenameDialog(
                initial = workspace.label,
                title = stringResource(R.string.manage_rename_project_title),
                // No "clear" for a project: a nameless row in the list says nothing, and the list is the only
                // place a project is ever seen from a phone.
                allowClear = false,
                onDismiss = { renaming = false },
                onConfirm = { label ->
                    renaming = false
                    onRename(label)
                },
            )
        }
        if (ending) {
            CloseProjectDialog(
                label = workspace.label.ifBlank { "project ${workspace.number}" },
                sessionCount = panes.size,
                onDismiss = { ending = false },
                onConfirm = {
                    ending = false
                    onEndProject()
                },
            )
        }
        panes.firstOrNull { it.paneId == endingPaneId }?.let { pane ->
            CloseSessionDialog(
                agentLabel = pane.displayAgent ?: pane.agent ?: stringResource(R.string.plain_shell),
                working = pane.agentStatus == AgentStates.WORKING,
                lastInProject = panes.size == 1,
                projectLabel = workspace.label.ifBlank { "project ${workspace.number}" },
                onDismiss = { endingPaneId = null },
                onConfirm = {
                    endingPaneId = null
                    onEndSession(pane.paneId, pane.hostId)
                },
            )
        }

        if (panes.isEmpty()) {
            Text(
                text = stringResource(R.string.no_sessions_in_project),
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.padding(16.dp),
            )
            return
        }

        LazyColumn(contentPadding = PaddingValues(16.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            items(panes, key = { "${it.hostId}/${it.paneId}" }) { pane ->
                Card(
                    modifier = Modifier.fillMaxWidth(),
                    colors = CardDefaults.cardColors(),
                ) {
                    Column(Modifier.padding(12.dp)) {
                        Text(
                            text = (pane.displayAgent ?: pane.agent ?: stringResource(R.string.plain_shell)) +
                                " · " + statusLabel(pane.agentStatus),
                            style = MaterialTheme.typography.titleSmall,
                        )
                        pane.terminalTitle?.takeIf { it.isNotBlank() }?.let { title ->
                            Text(text = title, style = MaterialTheme.typography.bodySmall)
                        }
                        pane.cwd?.takeIf { it.isNotBlank() }?.let { cwd ->
                            // Monospaced because a path is read character by
                            // character when you are trying to tell two of them
                            // apart.
                            Text(
                                text = cwd,
                                style = MaterialTheme.typography.labelSmall,
                                fontFamily = FontFamily.Monospace,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                        Row {
                            TextButton(onClick = { onOpenSession(pane.paneId, pane.hostId) }) {
                                Text(stringResource(R.string.open_the_session))
                            }
                            TextButton(onClick = { endingPaneId = pane.paneId }) {
                                Text(stringResource(R.string.manage_close_session))
                            }
                        }
                    }
                }
            }
        }
    }
}
