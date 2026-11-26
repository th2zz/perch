package dev.perch.console.features.manage

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import dev.perch.console.R
import dev.perch.console.core.herdrclient.BridgeException
import dev.perch.console.core.herdrclient.FoldersResponse
import dev.perch.console.core.herdrclient.Harness

object CreateRules {
    fun args(text: String): List<String> =
        text.lines().map { it.trimEnd('\r') }.filter { it.isNotBlank() }

    /**
     * Whether a project may be created.
     *
     * A blank [cwd] is the whole point: the form used to accept one and the bridge turned it into
     * the home folder, which is how a Claude session started in home and immediately asked whether
     * the folder was trusted. There is no default here — the folder is either chosen or the button
     * stays off.
     */
    fun canCreateProject(
        label: String,
        kind: String,
        cwd: String,
        busy: Boolean,
        harnessesBusy: Boolean,
        kindAvailable: Boolean,
    ): Boolean = label.isNotBlank() && kind.isNotBlank() && kindAvailable && cwd.isNotBlank() &&
        !busy && !harnessesBusy

    /**
     * Where the picker should open: the folder already chosen, else the last one used, else home.
     *
     * Home is a starting place for browsing, never a choice made on the person's behalf. Creating
     * still needs [canCreateProject] to see a real path afterwards.
     */
    fun startFolder(cwd: String, recentFolders: List<String>, homeFolder: String): String =
        cwd.trim().ifBlank {
            recentFolders.firstOrNull().orEmpty().trim().ifBlank { homeFolder.trim() }
        }

    /** True when the chosen folder is the home folder, which deserves the trust warning. */
    fun isHomeFolder(cwd: String, homeFolder: String): Boolean {
        val selected = folderKey(cwd)
        return selected.isNotEmpty() && selected == folderKey(homeFolder)
    }

    /**
     * Whether "Use this folder" may act on [manual].
     *
     * A typed path is not a chosen folder until Go has actually listed it: otherwise a person can
     * type `/work/new` and tap the confirm button while it still names the folder from before, which
     * looks exactly like the path they typed being accepted. The button waits for the listing.
     */
    fun canUseFolder(listingPath: String?, manual: String, busy: Boolean): Boolean =
        !busy && !listingPath.isNullOrBlank() && manual.trim() == listingPath.trim()

    /**
     * The same folder written two ways still means home.
     *
     * The bridge cleans a path it lists but reports its own home as the environment spells it, so
     * "/home/me" and "/home/me/" can meet in the same dialog. A trailing separator is the only
     * difference that matters here; root ("/") is kept as it is.
     */
    private fun folderKey(path: String): String {
        val trimmed = path.trim()
        return if (trimmed.length > 1) trimmed.trimEnd('/', '\\') else trimmed
    }
}

@Composable
fun NewProjectDialog(
    harnesses: List<Harness>,
    harnessesBusy: Boolean,
    busy: Boolean,
    homeFolder: String = "",
    recentFolders: List<String> = emptyList(),
    problem: String? = null,
    onListFolders: suspend (String) -> Result<FoldersResponse> = {
        Result.failure(IllegalStateException("folder listing is not connected"))
    },
    onDismiss: () -> Unit,
    onConfirm: (label: String, cwd: String, kind: String, name: String, args: List<String>) -> Unit,
) {
    var label by rememberSaveable { mutableStateOf("") }
    var cwd by rememberSaveable { mutableStateOf("") }
    var name by rememberSaveable { mutableStateOf("") }
    var argsText by rememberSaveable { mutableStateOf("") }
    var kind by rememberSaveable { mutableStateOf("") }
    var pickerOpen by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(harnesses) {
        if (harnesses.isNotEmpty() && harnesses.none { it.kind == kind }) {
            kind = harnesses.first().kind
        }
    }
    val home = homeFolder.trim()
    val selected = cwd.trim()
    val canCreate = CreateRules.canCreateProject(
        label = label,
        kind = kind,
        cwd = selected,
        busy = busy,
        harnessesBusy = harnessesBusy,
        // A restored dialog can remember a harness that is no longer installed. The list, not the
        // remembered string, decides whether Create may submit.
        kindAvailable = harnesses.any { it.kind == kind },
    )
    val shownFolder = selected.ifBlank { stringResource(R.string.new_project_no_folder) }
    val startAt = CreateRules.startFolder(selected, recentFolders, home)

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(stringResource(R.string.new_project_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = label,
                    onValueChange = { label = it },
                    singleLine = true,
                    label = { Text(stringResource(R.string.new_project_name)) },
                    modifier = Modifier.fillMaxWidth(),
                )
                // The working folder is a choice, not a field with a silent default. The path is
                // read-only here; it changes only through a picker action or the explicit home
                // shortcut, so a project cannot be created from whatever happened to be in a text
                // box (2026-10-21, issue #148: a blank folder used to mean home and started Claude
                // in it, which then asked whether the folder was trusted).
                Text(
                    text = stringResource(R.string.new_project_folder),
                    style = MaterialTheme.typography.labelLarge,
                )
                Text(
                    text = shownFolder,
                    style = MaterialTheme.typography.bodyMedium,
                    color = if (selected.isBlank()) {
                        MaterialTheme.colorScheme.onSurfaceVariant
                    } else {
                        MaterialTheme.colorScheme.onSurface
                    },
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(enabled = !busy, onClick = { pickerOpen = true }) {
                        Text(stringResource(R.string.new_project_choose_folder))
                    }
                    if (home.isNotBlank()) {
                        TextButton(enabled = !busy, onClick = { cwd = home }) {
                            Text(stringResource(R.string.new_project_use_home))
                        }
                    }
                }
                if (CreateRules.isHomeFolder(selected, home)) {
                    Text(
                        text = stringResource(R.string.new_project_home_warning),
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
                HarnessPicker(
                    harnesses = harnesses,
                    loading = harnessesBusy,
                    enabled = !busy,
                    kind = kind,
                    onChange = { kind = it },
                )
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    singleLine = true,
                    label = { Text(stringResource(R.string.new_session_name)) },
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = argsText,
                    onValueChange = { argsText = it },
                    label = { Text(stringResource(R.string.new_session_args)) },
                    supportingText = { Text(stringResource(R.string.new_session_args_hint)) },
                    modifier = Modifier.fillMaxWidth(),
                )
                if (busy) {
                    Text(stringResource(R.string.new_session_creating))
                } else if (problem != null) {
                    Text(
                        text = problem,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = canCreate,
                onClick = {
                    onConfirm(label.trim(), selected, kind, name.trim(), CreateRules.args(argsText))
                },
            ) {
                Text(stringResource(R.string.new_session_create))
            }
        },
        dismissButton = {
            TextButton(enabled = !busy, onClick = onDismiss) {
                Text(stringResource(R.string.manage_cancel))
            }
        },
    )

    if (pickerOpen) {
        FolderPickerDialog(
            startAt = startAt,
            homeFolder = home,
            recentFolders = recentFolders,
            onListFolders = onListFolders,
            onDismiss = { pickerOpen = false },
            onChoose = { chosen ->
                cwd = chosen
                pickerOpen = false
            },
        )
    }
}

/**
 * One directory on the computer, with the three ways out of it that a person needs.
 *
 * The picker is deliberately not a VSCode-style tree: it shows one folder at a time, with Up, Home,
 * recent folders, and a manual path for somewhere the rows do not reach. That is enough to choose a
 * project folder on a phone without turning a dialog into a file manager.
 */
@Composable
private fun FolderPickerDialog(
    startAt: String,
    homeFolder: String,
    recentFolders: List<String>,
    onListFolders: suspend (String) -> Result<FoldersResponse>,
    onDismiss: () -> Unit,
    onChoose: (String) -> Unit,
) {
    var target by rememberSaveable { mutableStateOf(startAt) }
    var manual by rememberSaveable { mutableStateOf(startAt) }
    var listing by remember { mutableStateOf<FoldersResponse?>(null) }
    // True from the first frame: the effect below always asks the computer once, and an empty list
    // that is really "not answered yet" must not be drawn as "No folders here."
    var busy by remember { mutableStateOf(true) }
    var problem by remember { mutableStateOf<String?>(null) }
    // Lets "Go" ask again for the folder already on screen, which is what a person does after
    // creating the folder in another app and coming back to this dialog.
    var attempt by remember { mutableIntStateOf(0) }
    val fallbackProblem = stringResource(R.string.manage_failed)
    val fallbackFix = stringResource(R.string.folder_picker_failed_fix)

    LaunchedEffect(target, attempt) {
        busy = true
        problem = null
        onListFolders(target)
            .onSuccess { view ->
                listing = view
                // Do not erase what the person is typing while the request was in flight. When
                // they have not touched the field (or it still names the folder we asked for),
                // show the canonical path the computer answered.
                if (view.path.isNotBlank() && manual.trim() == target.trim()) {
                    manual = view.path
                }
            }
            .onFailure { failure ->
                listing = null
                val reason = failure.message?.takeIf { it.isNotBlank() } ?: fallbackProblem
                val fix = (failure as? BridgeException)?.fix?.takeIf { it.isNotBlank() }
                    ?: fallbackFix
                problem = "$reason\n\n$fix"
            }
        busy = false
    }

    val current = listing?.path ?: target
    val parent = listing?.parent.orEmpty()
    val home = listing?.home?.takeIf { it.isNotBlank() } ?: homeFolder
    val entries = listing?.entries.orEmpty()
    val canUse = CreateRules.canUseFolder(listing?.path, manual, busy)
    val emptyFolder = stringResource(R.string.folder_picker_empty)
    val manualPending = manual.trim().isNotEmpty() && manual.trim() != current.trim()
    // Clearing the listing as soon as a new path is asked for is what keeps "Use this folder" from
    // naming the folder that was on screen a moment ago: the button is disabled until the new
    // directory has actually answered.
    val navigate: (String) -> Unit = { path ->
        target = path
        manual = path
        listing = null
        problem = null
        busy = true
        // A request id, not just the path: tapping Home (or a recent folder) while already there
        // leaves `target` unchanged, and without this the effect would never re-run — leaving the
        // picker busy forever with Cancel disabled. Every navigation is a new request.
        attempt++
    }

    AlertDialog(
        // A read is dismissible. Only creation is not: the picker asked the computer a question,
        // and waiting for an answer must not trap the person in a modal they cannot leave when
        // the computer is asleep.
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.folder_picker_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    text = stringResource(R.string.folder_picker_current),
                    style = MaterialTheme.typography.labelMedium,
                )
                Text(
                    text = current.ifBlank { stringResource(R.string.folder_picker_loading) },
                    style = MaterialTheme.typography.bodySmall,
                )
                Row(
                    horizontalArrangement = Arrangement.spacedBy(4.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    TextButton(
                        enabled = parent.isNotBlank() && !busy,
                        onClick = { navigate(parent) },
                    ) {
                        Text(stringResource(R.string.folder_picker_up))
                    }
                    TextButton(
                        enabled = home.isNotBlank() && !busy,
                        onClick = { navigate(home) },
                    ) {
                        Text(stringResource(R.string.folder_picker_home))
                    }
                }
                if (recentFolders.isNotEmpty()) {
                    Text(
                        text = stringResource(R.string.folder_picker_recent),
                        style = MaterialTheme.typography.labelMedium,
                    )
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .horizontalScroll(rememberScrollState()),
                        horizontalArrangement = Arrangement.spacedBy(4.dp),
                    ) {
                        recentFolders.take(4).forEach { folder ->
                            TextButton(enabled = !busy, onClick = { navigate(folder) }) {
                                Text(
                                    // The whole path, not just the last segment: two projects can
                                    // be called "work", and a recent row that cannot tell them
                                    // apart invites the wrong tap.
                                    text = folder,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                )
                            }
                        }
                    }
                }
                OutlinedTextField(
                    value = manual,
                    onValueChange = { manual = it },
                    singleLine = true,
                    label = { Text(stringResource(R.string.folder_picker_manual)) },
                    // The keyboard's Go key is the commit. A separate on-screen Go
                    // button duplicated what the field already said and made the
                    // folder rows underneath look like they were not the main way
                    // through the picker.
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                    keyboardActions = KeyboardActions(
                        onGo = {
                            if (manual.isNotBlank() && !busy) navigate(manual.trim())
                        },
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )
                if (manualPending && !busy) {
                    Text(
                        text = stringResource(R.string.folder_picker_press_go),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (busy) {
                    Text(
                        text = stringResource(R.string.folder_picker_loading),
                        style = MaterialTheme.typography.bodySmall,
                    )
                } else if (problem != null) {
                    Text(
                        text = problem.orEmpty(),
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                    )
                } else {
                    if (entries.isNotEmpty()) {
                        Text(
                            text = stringResource(R.string.folder_picker_subfolders),
                            style = MaterialTheme.typography.labelLarge,
                        )
                        Text(
                            text = stringResource(R.string.folder_picker_tap_to_open),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    LazyColumn(
                        modifier = Modifier.fillMaxWidth().heightIn(max = 220.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp),
                    ) {
                        if (entries.isEmpty()) {
                            item { Text(text = emptyFolder, style = MaterialTheme.typography.bodySmall) }
                        }
                        items(entries, key = { it.path }) { entry ->
                            // A folder row, not a plain text button: the tinted
                            // surface and chevron make it visible that tapping
                            // goes *into* the directory; choosing the current
                            // directory stays the "Use this folder" action below.
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clip(RoundedCornerShape(8.dp))
                                    .background(
                                        MaterialTheme.colorScheme.surfaceVariant.copy(alpha = 0.45f),
                                    )
                                    .clickable(enabled = !busy) { navigate(entry.path) }
                                    .padding(horizontal = 12.dp, vertical = 10.dp),
                                verticalAlignment = Alignment.CenterVertically,
                            ) {
                                Text(
                                    text = entry.name,
                                    style = MaterialTheme.typography.bodyLarge,
                                    maxLines = 1,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f),
                                )
                                Text(
                                    text = "›",
                                    style = MaterialTheme.typography.titleLarge,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                )
                            }
                        }
                    }
                    if (listing?.truncated == true) {
                        Text(
                            text = stringResource(R.string.folder_picker_truncated),
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        },
        confirmButton = {
            TextButton(enabled = canUse, onClick = { onChoose(current) }) {
                Text(stringResource(R.string.folder_picker_use))
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) {
                Text(stringResource(R.string.manage_cancel))
            }
        },
    )
}

@Composable
fun NewSessionDialog(
    projectLabel: String,
    harnesses: List<Harness>,
    harnessesBusy: Boolean,
    busy: Boolean,
    problem: String? = null,
    onDismiss: () -> Unit,
    onConfirm: (kind: String, name: String, args: List<String>) -> Unit,
) {
    var name by rememberSaveable { mutableStateOf("") }
    var argsText by rememberSaveable { mutableStateOf("") }
    var kind by rememberSaveable { mutableStateOf("") }
    LaunchedEffect(harnesses) {
        if (harnesses.isNotEmpty() && harnesses.none { it.kind == kind }) {
            kind = harnesses.first().kind
        }
    }
    val canCreate = kind.isNotBlank() && !busy && !harnessesBusy

    AlertDialog(
        onDismissRequest = { if (!busy) onDismiss() },
        title = { Text(stringResource(R.string.new_session_title, projectLabel)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                HarnessPicker(
                    harnesses = harnesses,
                    loading = harnessesBusy,
                    enabled = !busy,
                    kind = kind,
                    onChange = { kind = it },
                )
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    singleLine = true,
                    label = { Text(stringResource(R.string.new_session_name)) },
                    modifier = Modifier.fillMaxWidth(),
                )
                OutlinedTextField(
                    value = argsText,
                    onValueChange = { argsText = it },
                    label = { Text(stringResource(R.string.new_session_args)) },
                    supportingText = { Text(stringResource(R.string.new_session_args_hint)) },
                    modifier = Modifier.fillMaxWidth(),
                )
                if (busy) {
                    Text(stringResource(R.string.new_session_creating))
                } else if (problem != null) {
                    Text(
                        text = problem,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodySmall,
                    )
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = canCreate,
                onClick = { onConfirm(kind, name.trim(), CreateRules.args(argsText)) },
            ) {
                Text(stringResource(R.string.new_session_create))
            }
        },
        dismissButton = {
            TextButton(enabled = !busy, onClick = onDismiss) {
                Text(stringResource(R.string.manage_cancel))
            }
        },
    )
}

@Composable
private fun HarnessPicker(
    harnesses: List<Harness>,
    loading: Boolean,
    enabled: Boolean,
    kind: String,
    onChange: (String) -> Unit,
) {
    var open by rememberSaveable { mutableStateOf(false) }
    Column {
        Box {
            OutlinedButton(
                enabled = harnesses.isNotEmpty() && enabled,
                onClick = { open = true },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(kind.ifBlank { stringResource(R.string.new_session_choose_harness) })
            }
            DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
                harnesses.forEach { harness ->
                    DropdownMenuItem(
                        text = { Text(harness.kind) },
                        onClick = {
                            onChange(harness.kind)
                            open = false
                        },
                    )
                }
            }
        }
        if (loading) {
            Text(
                text = stringResource(R.string.new_session_loading_harnesses),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
        } else if (harnesses.isEmpty()) {
            Text(
                text = stringResource(R.string.new_session_no_harness),
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = 4.dp),
            )
        }
    }
}
