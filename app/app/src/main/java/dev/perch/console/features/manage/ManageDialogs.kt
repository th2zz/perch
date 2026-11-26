package dev.perch.console.features.manage

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.AlertDialog
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
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.herdrclient.ManageLabels

/**
 * The two conversations this product has with somebody before it changes their workspace.
 *
 * Naming is cheap and reversible, so its dialog is a text field and a Save. Ending something is neither, so
 * its dialogs spend their words on *what will be lost*: what the agent was doing, whether this is the last
 * session in a project (which closes the project too — measured on a live herdr, 2026-10-15), and that herdr
 * has no undo.
 *
 * Both are drawn as dialogs rather than screens on purpose: they are answers to a question the user asked
 * ("rename this"), and a screen would make them feel like a place they navigated to rather than a decision
 * they are making.
 */
@Composable
fun RenameDialog(
    /** The name it has now, so the field starts where the user left off rather than empty. */
    initial: String,
    /** True for a project, which cannot have its name cleared — a nameless row in a list says nothing. */
    title: String,
    allowClear: Boolean,
    onDismiss: () -> Unit,
    onConfirm: (String) -> Unit,
    /** A session's name can be cleared; a project's cannot. Null hides the button. */
    onClear: (() -> Unit)? = null,
) {
    var text by remember { mutableStateOf(initial) }
    val problem = ManageLabels.problem(text)

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(title) },
        text = {
            Column {
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it },
                    singleLine = true,
                    isError = problem != null && text.isNotEmpty(),
                    modifier = Modifier.fillMaxWidth(),
                )
                Text(
                    text = problem ?: stringResource(R.string.manage_rename_hint),
                    style = MaterialTheme.typography.labelSmall,
                    color = if (problem != null) {
                        MaterialTheme.colorScheme.error
                    } else {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    },
                    modifier = Modifier.padding(top = 6.dp),
                )
            }
        },
        confirmButton = {
            TextButton(
                // Disabled rather than validated on press: the sentence above the button already says what is
                // wrong, and a button that refuses to act without saying why is the thing this app is against.
                enabled = problem == null,
                onClick = { onConfirm(text.trim()) },
            ) {
                Text(stringResource(R.string.manage_rename_save))
            }
        },
        dismissButton = {
            Column {
                if (allowClear && onClear != null) {
                    TextButton(onClick = onClear) {
                        Text(stringResource(R.string.manage_rename_clear))
                    }
                }
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.manage_cancel)) }
            }
        },
    )
}

/**
 * Confirming the end of one session.
 *
 * [working] is the fact that matters and the reason this dialog is not a yes/no formality: an idle session is
 * a thing you are tidying up, and a *working* one is a thing you are about to interrupt. [lastInProject]
 * carries the other fact worth knowing before the tap — closing it takes the project with it.
 */
@Composable
fun CloseSessionDialog(
    agentLabel: String,
    working: Boolean,
    lastInProject: Boolean,
    projectLabel: String,
    onDismiss: () -> Unit,
    onConfirm: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.manage_close_session_title)) },
        text = {
            Column {
                Text(stringResource(R.string.manage_close_session_body))
                // Which warnings to show is a decision, and it is made in [ManageCopy] where it can be tested in
                // every combination without drawing a dialog (see the note there). This only draws them.
                for (warning in ManageCopy.closeSessionWarnings(working, lastInProject)) {
                    Text(
                        text = when (warning) {
                            ManageCopy.Warning.AgentIsWorking ->
                                stringResource(R.string.manage_close_session_working, agentLabel)

                            ManageCopy.Warning.LastSessionInProject ->
                                stringResource(R.string.manage_close_last_session, projectLabel)
                        },
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(stringResource(R.string.manage_close_session_confirm))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.manage_cancel)) }
        },
    )
}

/** Confirming the end of a whole project, with how many sessions go with it. */
@Composable
fun CloseProjectDialog(
    label: String,
    sessionCount: Int,
    onDismiss: () -> Unit,
    onConfirm: () -> Unit,
) {
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.manage_close_project_title, label)) },
        text = {
            Column {
                Text(stringResource(R.string.manage_close_project_body))
                if (ManageCopy.projectCloseNamesCount(sessionCount)) {
                    Text(
                        // A quantity string, so one session is one session: "1 sessions" in the sentence that
                        // tells somebody what they are about to destroy reads like a bug (found on a device).
                        text = pluralStringResource(
                            R.plurals.manage_close_project_count,
                            sessionCount,
                            sessionCount,
                        ),
                        color = MaterialTheme.colorScheme.error,
                        modifier = Modifier.padding(top = 8.dp),
                    )
                }
            }
        },
        confirmButton = {
            TextButton(onClick = onConfirm) {
                Text(stringResource(R.string.manage_close_project_confirm))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text(stringResource(R.string.manage_cancel)) }
        },
    )
}
