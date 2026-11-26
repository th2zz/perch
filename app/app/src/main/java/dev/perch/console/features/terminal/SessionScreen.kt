package dev.perch.console.features.terminal

import androidx.compose.animation.core.LinearEasing
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AssistChip
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.snapshotFlow
import androidx.activity.compose.BackHandler
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.dp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import dev.perch.console.R
import dev.perch.console.core.format.UsageSummary
import dev.perch.console.core.herdrclient.AgentStates
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.Transcript
import dev.perch.console.core.herdrclient.UsageFacts
import dev.perch.console.features.hosts.statusLabel
import dev.perch.console.features.manage.CloseSessionDialog
import dev.perch.console.features.manage.RenameDialog

/**
 * The escape hatch: one session, read-only, plus the right to type into it.
 *
 * This screen exists because the product's most important rule has a cost. Refusing
 * to guess which keys a dialog wants is the right call, but a refusal with nothing
 * behind it strands a user who is away from their desk — which is the exact
 * situation the product is for. So a refusal hands them here instead.
 *
 * Three deliberate limits, all of them the same limit:
 *
 *  - It is not a terminal emulator. The text is read, not interpreted, and there
 *    is no VT parsing, no scrollback model, no cursor. Competing with a real
 *    terminal is a fight this product cannot win and does not want.
 *  - The keys are not here at all any more. They belong to the terminal screen, which has a real
 *    one — see [RealTerminalScreen] and [TerminalKeys]: Escape, Tab, arrows and Ctrl are what a
 *    dialog asks for, and they now type into a terminal instead of being forwarded as key names.
 *    When the computer could not identify the dialog, the screen says so and
 *    leaves the presses to the person — which is the honest version of "we will
 *    not guess on your behalf": it is not a dead end, it is their terminal.
 *  - Typing is the fourth write class, unchanged: the sentence goes to the pane
 *    exactly as the user wrote it, and nothing is sent until they send it.
 */
@Composable
fun SessionScreen(
    session: SessionView,
    busy: Boolean,
    onApprove: () -> Unit,
    onReject: () -> Unit,
    onSend: (String) -> Unit,
    onRefresh: () -> Unit,
    onClose: () -> Unit,
    modifier: Modifier = Modifier,
    onVisible: (Boolean) -> Unit = {},
    /**
     * Whether the reader is at the end of the session.
     *
     * The screen decides this (it is a fact about a scroll position) and reports it,
     * because the state holder needs it to keep refreshing reads from replacing the text
     * the user is reading.
     */
    onFollowing: (Boolean) -> Unit = {},
    /**
     * What the harness itself knows about this session, and whether the computer is still
     * being asked.
     *
     * Passed in rather than fetched here because it is not part of the screen's job: the state
     * holder owns the request, and the screen renders whatever it has. That also makes the
     * panel testable with facts a test made up, which is how its layout gets looked at.
     */
    usage: UsageFacts? = null,
    usageBusy: Boolean = false,
    onLoadUsage: () -> Unit = {},
    /**
     * The session's conversation as the agent's own record has it, and whether the computer has one
     * to give.
     *
     * The reading area draws this when [Transcript.readable], and the terminal's own text otherwise.
     * Passed in rather than fetched here for the same reason the usage panel is: the state holder owns
     * the request, and the screen renders what it has — which is also what makes this face testable
     * with a conversation a test wrote.
     */
    conversation: Transcript? = null,
    /**
     * Opens the session's real terminal: the pane's own byte stream, drawn by an emulator.
     *
     * A factory rather than a live connection, because the screen owns the lifetime — and because a
     * *fresh* attach is what makes this safe: the terminal repaints the screen as it is now, so there
     * is nothing to replay and nothing to get out of step.
     */
    connectTerminal: () -> TerminalConnection? = { null },
    /**
     * One line about the terminal screen, for the computer's log (see [RealTerminalScreen]).
     *
     * Passed down rather than called here because the state holder is what holds a client; this screen
     * only knows it has something worth saying.
     */
    onTerminalReport: (event: String, detail: String) -> Unit = { _, _ -> },
    /**
     * Sends a picture of the terminal screen to the computer (see [RealTerminalScreen]).
     *
     * Passed down rather than posted here because the state holder is what holds a client, exactly like the
     * terminal's own log lines: this screen only knows it has something to show.
     */
    onSendScreenshot: (png: ByteArray) -> Unit = {},
    /**
     * Whether this session should open straight into the terminal.
     *
     * True when it was opened from a card that could not press its dialog: the user is standing in
     * front of a question nobody could answer for them, and the terminal is where the keys are.
     */
    openTerminalOnOpen: Boolean = false,
    terminalOnly: Boolean = false,
    /**
     * Names this session, or clears its name when the label is blank.
     *
     * Renaming and ending a session are the two actions that change the user's workspace rather than the
     * conversation in it (2026-10-15, asked for from the phone). They arrive as callbacks rather than being
     * done here for the same reason everything else does: this screen renders, the state holder talks.
     */
    onRename: (String) -> Unit = {},
    /**
     * Ends this session on the computer — which is a different thing from [onClose], and the difference is
     * worth the awkward pair of names: that one leaves the screen, this one stops the program.
     */
    onEndSession: () -> Unit = {},
    /**
     * Whether this is the only session in its project.
     *
     * Measured on a live herdr (2026-10-15): closing the last session of a project closes the project, because
     * herdr has no such thing as an empty project. The dialog says so before the tap, and this is how it knows.
     */
    lastInProject: Boolean = false,
) {
    var draft by rememberSaveable { mutableStateOf("") }
    // The sentence waiting on the confirmation below, when the terminal's own input
    // line is not empty. Held separately from the draft because the two mean different
    // things: the draft is what the user is editing, this is what they have handed over.
    var pendingAppend by rememberSaveable(session.paneId) { mutableStateOf<String?>(null) }
    BackHandler(enabled = true) { onClose() }
    val scroll = rememberScrollState()
    var terminalOpen by rememberSaveable(session.paneId) { mutableStateOf(openTerminalOnOpen) }
    // Which of the two manage dialogs is open, if either. Neither is remembered across a rotation:
    // a confirmation that survives a turn of the phone is a confirmation nobody gave.
    var renaming by remember { mutableStateOf(false) }
    var ending by remember { mutableStateOf(false) }
    if (terminalOnly || terminalOpen) {
        RealTerminalScreen(
            session = session,
            connect = connectTerminal,
            onReport = onTerminalReport,
            onSendScreenshot = onSendScreenshot,
            onClose = { if (terminalOnly) onClose() else terminalOpen = false },
        )
        return
    }

    // Live: told from the screen's own lifecycle rather than from the state holder,
    // because the state holder outlives the screen in both directions — it survives
    // the app going to the background, and it is not recreated when a second session
    // is opened. The activity's STARTED/STOPPED pair is exactly "is this on screen".
    val lifecycleOwner = LocalLifecycleOwner.current
    DisposableEffect(lifecycleOwner) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_START -> onVisible(true)
                Lifecycle.Event.ON_STOP -> onVisible(false)
                else -> Unit
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        // The screen can arrive while the activity is already started, in which case
        // no event is coming — the answer is the lifecycle's current state.
        onDispose {
            lifecycleOwner.lifecycle.removeObserver(observer)
            onVisible(false)
        }
    }

    var follow by rememberSaveable(session.paneId) { mutableStateOf(true) }
    val followLatestLabel = stringResource(R.string.session_follow_latest)
    // True while *we* are moving the view. A programmatic scroll and a finger look the
    // same from the outside — both flip `isScrollInProgress` and both walk the offset — and
    // the first attempt at a gliding catch-up proved it: the animation's own intermediate
    // positions were read as "the reader scrolled away", so the screen stopped following
    // itself. So the one thing that distinguishes them is said out loud here.
    var catchingUp by remember(session.paneId) { mutableStateOf(false) }
    LaunchedEffect(scroll, session.paneId) {
        snapshotFlow { scroll.isScrollInProgress to scroll.value }
            .collect { (inProgress, offset) ->
                if (inProgress && !catchingUp) {
                    follow = LiveSession.following(offset, scroll.maxValue)
                }
            }
    }
    LaunchedEffect(follow) { onFollowing(follow) }
    LaunchedEffect(follow, session.paneId, session.text) {
        if (!follow) return@LaunchedEffect
        if (scroll.value == scroll.maxValue) return@LaunchedEffect
        catchingUp = true
        try {
            scroll.animateScrollTo(
                value = scroll.maxValue,
                animationSpec = tween(
                    durationMillis = LiveSession.FOLLOW_SCROLL_MILLIS,
                    easing = LinearEasing,
                ),
            )
        } finally {
            catchingUp = false
        }
    }

    Surface(modifier = modifier.fillMaxSize()) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                // safeDrawing is the status bar, the navigation bar and the keyboard
                // together. It is here because the first real trial found this screen
                // by eye: with the app drawn edge to edge, a bare Column puts its top
                // row underneath the system's signal and battery icons, and the close
                // and refresh controls end up crammed against them (2026-10-13,
                // HyperOS). The console screen did not have this problem because it
                // uses Scaffold + TopAppBar, which apply the inset themselves — which
                // is also why no test here could see it: the rendered screenshots run
                // with zero insets.
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                SessionLight(session)
                Column(Modifier.weight(1f)) {
                    Text(
                        text = session.agentLabel,
                        style = MaterialTheme.typography.titleMedium,
                    )
                    Text(
                        // The computer leads when the session is somewhere else:
                        // "which machine am I about to type into" is the one thing
                        // a user must never have to guess (spec FR-016).
                        text = listOfNotNull(
                            session.hostLabel
                                .takeIf { it.isNotBlank() && session.hostId != Host.LOCAL },
                            session.workspaceLabel.takeIf { it.isNotBlank() },
                            session.agentStatus.takeIf { it.isNotBlank() },
                        ).joinToString(" · "),
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                if (busy) CircularProgressIndicator()
                SessionMenu(
                    onOpenTerminal = { terminalOpen = true },
                    session = session,
                    usage = usage,
                    usageBusy = usageBusy,
                    onLoadUsage = onLoadUsage,
                    onRefresh = onRefresh,
                    onClose = onClose,
                    onRename = { renaming = true },
                    onEndSession = { ending = true },
                )
                // Both dialogs live here rather than in the menu, because a menu that closes over its own
                // dialog is a menu that has to be reopened to see the answer to what it asked.
                if (renaming) {
                    RenameDialog(
                        initial = session.title,
                        title = stringResource(R.string.manage_rename_title),
                        // A session's name can be cleared — that is how a bad one is undone, and herdr shows
                        // what the program calls itself again.
                        allowClear = true,
                        onDismiss = { renaming = false },
                        onConfirm = { label ->
                            renaming = false
                            onRename(label)
                        },
                        onClear = {
                            renaming = false
                            onRename("")
                        },
                    )
                }
                if (ending) {
                    CloseSessionDialog(
                        agentLabel = session.agentLabel,
                        working = session.agentStatus == AgentStates.WORKING,
                        lastInProject = lastInProject,
                        projectLabel = session.workspaceLabel,
                        onDismiss = { ending = false },
                        onConfirm = {
                            ending = false
                            onEndSession()
                        },
                    )
                }
            }

            if (session.setupRequired) {
                SetupRequiredNotice(
                    session = session,
                    onOpenTerminal = { terminalOpen = true },
                )
            }

            // Selection is not decoration: the most common reason to open this
            // screen is to copy an error message out of it.
            Box(modifier = Modifier.weight(1f)) {
                SelectionContainer {
                    Column(
                        modifier = Modifier
                            .fillMaxWidth()
                            .verticalScroll(scroll),
                    ) {
                        // Two sources, one area, and the order between them is the whole design.
                        //
                        // The agent's own record first: it can say exactly what the user asked and
                        // exactly what the agent answered, because it was written by the program that
                        // knows — see [ConversationFace] for what that fixed. The terminal's own text
                        // is the fallback rather than the default, and only when there is no record to
                        // read: an agent nobody has taught Perch to read is still a session worth
                        // opening, and the screen's text is what this face showed before records
                        // existed.
                        //
                        // Nothing is drawn from both. The two say the same words differently, and a
                        // reading area that showed one under the other would be worse than either.
                        if (conversation != null && conversation.readable) {
                            ConversationFace(conversation)
                        } else {
                            ScreenText(
                                text = session.conversation.ifBlank { stringResource(R.string.session_empty) },
                                modifier = Modifier.fillMaxWidth(),
                            )
                        }
                    }
                }

                if (!follow) {
                    OutlinedButton(
                        onClick = { follow = true },
                        modifier = Modifier
                            .align(Alignment.BottomEnd)
                            .padding(8.dp)
                            .semantics { contentDescription = followLatestLabel },
                    ) {
                        Text(followLatestLabel)
                    }
                }
            }

            if (session.truncated) {
                Text(
                    text = stringResource(R.string.session_truncated),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            HorizontalDivider()
            // Above the row, not below it: the sentence explains the row, and a
            // sentence that points at "the keys below" has to be above them.
            ActionRow(
                session = session,
                busy = busy,
                onApprove = onApprove,
                onReject = onReject,
            )

            if (session.unsent.isNotBlank()) {
                Text(
                    text = stringResource(R.string.session_unsent, session.unsent),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }

            if (session.menu) {
                Text(
                    text = stringResource(R.string.session_menu),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.error,
                )
            }

            if (!session.setupRequired) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.Bottom,
                ) {
                    OutlinedTextField(
                        value = draft,
                        onValueChange = { draft = it },
                        modifier = Modifier.weight(1f),
                        placeholder = { Text(stringResource(R.string.session_input_hint)) },
                        singleLine = false,
                        maxLines = 4,
                    )
                    TextButton(
                        onClick = {
                            // Appending to text that is already in the terminal is not a
                            // thing to do by accident, so it takes one more tap — the same
                            // rule as everywhere else in this product: never guess, and never
                            // act on the user's behalf on a screen we do not own.
                            if (session.unsent.isBlank()) {
                                val sending = draft
                                // Cleared only after handing it over, so a refused send does
                                // not silently eat what the user typed.
                                draft = ""
                                onSend(sending)
                            } else {
                                // Held, not cleared: a cancelled confirmation has to leave the
                                // user's sentence where they left it.
                                pendingAppend = draft
                            }
                        },
                        // Nothing is typed into a menu: it would be driven by us instead of by
                        // the person in front of it (see the notice above).
                        enabled = !busy && draft.isNotBlank() && !session.menu &&
                            session.canDeliverText,
                    ) {
                        Text(stringResource(R.string.compose_send))
                    }
                }
            }

            pendingAppend?.let { pending ->
                AlertDialog(
                    onDismissRequest = { pendingAppend = null },
                    title = {
                        Text(
                            stringResource(
                                if (session.unsentCommand) R.string.session_unsent_command_title
                                else R.string.session_unsent_title,
                            ),
                        )
                    },
                    text = {
                        Text(
                            stringResource(
                                if (session.unsentCommand) R.string.session_unsent_command_body
                                else R.string.session_unsent_body,
                                session.unsent,
                                pending,
                            ),
                        )
                    },
                    confirmButton = {
                        TextButton(
                            onClick = {
                                pendingAppend = null
                                draft = ""
                                onSend(pending)
                            },
                        ) {
                            Text(stringResource(R.string.session_unsent_send_anyway))
                        }
                    },
                    dismissButton = {
                        TextButton(onClick = { pendingAppend = null }) {
                            Text(stringResource(R.string.session_unsent_cancel))
                        }
                    },
                )
            }

            // The shell warning stays, and it is the only sentence left down here:
            // it is not a description of the state (the light in the corner does
            // that) but a statement about what sending *does* — to a shell a line
            // break is an Enter, and the user should know that before they send a
            // two-line message (measured, see scripts/verify-us3.sh).
            if (session.hasNoAgent) {
                Text(
                    text = stringResource(R.string.session_note_shell),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

/**
 * The action row: one line, whatever else is on it.
 *
 * Approve and reject are pinned at the leading edge rather than inside the
 * scrolling part, and that is a decision about the three fastest taps in the
 * product (spec SC-002): a button the user has to scroll to find is not a
 * one-tap approval. The keys scroll, they do not.
 */
@Composable
private fun SetupRequiredNotice(
    session: SessionView,
    onOpenTerminal: () -> Unit,
) {
    Surface(
        color = MaterialTheme.colorScheme.errorContainer,
        shape = MaterialTheme.shapes.medium,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(
            modifier = Modifier.padding(12.dp),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            Text(
                text = stringResource(R.string.session_setup_title),
                style = MaterialTheme.typography.titleSmall,
            )
            Text(
                text = session.setupReason.ifBlank {
                    stringResource(R.string.session_setup_fallback)
                },
                style = MaterialTheme.typography.bodySmall,
            )
            if (session.setupFix.isNotBlank()) {
                Text(
                    text = session.setupFix,
                    style = MaterialTheme.typography.labelSmall,
                )
            }
            OutlinedButton(onClick = onOpenTerminal) {
                Text(stringResource(R.string.session_setup_open_terminal))
            }
        }
    }
}

@Composable
private fun ActionRow(
    session: SessionView,
    busy: Boolean,
    onApprove: () -> Unit,
    onReject: () -> Unit,
) {
    val anythingToShow = session.approvable || session.rejectable
    if (!anythingToShow) return

    Row(
        modifier = Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        if (session.approvable) {
            OutlinedButton(onClick = onApprove, enabled = !busy) {
                Text(stringResource(R.string.approve))
            }
        }
        if (session.rejectable) {
            OutlinedButton(onClick = onReject, enabled = !busy) {
                Text(stringResource(R.string.reject))
            }
        }
    }
}

/**
 * Why there is no one-tap answer for this session, when there is not.
 *
 * Three different situations, and the difference matters to what the user does
 * next: a session with no agent in it, a session that is not asking anything, and
 * — the one this screen exists for — a dialog the computer cannot identify. Only
 * in the first two is the computer's own sentence the right one to show; in the
 * third it would be a report about a key table, and the useful thing to say is
 * that the keys below are theirs to press. One line, because the user reads this
 * once and then lives with it every time they open a session.
 */
@Composable
private fun reasonFor(session: SessionView): String? {
    if (session.approvable || session.rejectable || session.answerable) return null
    if (session.agentStatus != AgentStates.BLOCKED) return null
    return stringResource(R.string.session_no_one_tap)
}

@Composable
private fun SessionLight(session: SessionView) {
    val colour = when {
        session.waitingOnAPerson -> MaterialTheme.colorScheme.error
        session.agentStatus == AgentStates.WORKING -> MaterialTheme.colorScheme.primary
        session.hasNoAgent -> MaterialTheme.colorScheme.outline
        else -> MaterialTheme.colorScheme.onSurfaceVariant
    }
    val description = statusLabel(session.agentStatus)
    val alpha = if (session.agentStatus == AgentStates.WORKING) {
        val transition = rememberInfiniteTransition(label = "working")
        val pulse by transition.animateFloat(
            initialValue = 1f,
            targetValue = 0.3f,
            animationSpec = infiniteRepeatable(
                animation = tween(durationMillis = 700, easing = LinearEasing),
                repeatMode = RepeatMode.Reverse,
            ),
            label = "pulse",
        )
        pulse
    } else {
        1f
    }
    Box(
        modifier = Modifier
            .size(10.dp)
            .clip(CircleShape)
            .background(colour.copy(alpha = alpha))
            .semantics { contentDescription = description },
    )
}

@Composable
private fun SessionMenu(
    onOpenTerminal: () -> Unit,
    session: SessionView,
    usage: UsageFacts?,
    usageBusy: Boolean,
    onLoadUsage: () -> Unit,
    onRefresh: () -> Unit,
    onClose: () -> Unit,
    onRename: () -> Unit,
    onEndSession: () -> Unit,
) {
    var open by remember { mutableStateOf(false) }
    var usageOpen by remember { mutableStateOf(false) }
    Box {
        TextButton(onClick = { open = true }) {
            Text(
                text = "⋮",
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.semantics { contentDescription = "Menu" },
            )
        }
        DropdownMenu(expanded = open, onDismissRequest = { open = false }) {
            DropdownMenuItem(
                text = { Text(stringResource(R.string.session_usage)) },
                onClick = {
                    open = false
                    usageOpen = true
                    onLoadUsage()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.session_terminal)) },
                onClick = {
                    open = false
                    onOpenTerminal()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.session_refresh)) },
                onClick = {
                    open = false
                    onRefresh()
                },
            )
            // Naming and ending this session, at the bottom of the menu and behind the ⋮: everything above
            // them is what somebody opened the session screen to do.
            //
            // **These two items were missing from the first version of this feature** — the callbacks arrived
            // in this function, the dialogs were built, the tests passed, and the menu had no way to reach
            // either of them. Found by walking the app on the emulator (2026-10-15), which is exactly the kind
            // of gap no unit test on this machine was going to notice: nothing was broken, a thing was absent.
            // `SessionMenuTest` now holds the door open.
            DropdownMenuItem(
                text = { Text(stringResource(R.string.manage_rename)) },
                onClick = {
                    open = false
                    onRename()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.manage_close_session)) },
                onClick = {
                    open = false
                    onEndSession()
                },
            )
            DropdownMenuItem(
                text = { Text(stringResource(R.string.session_close)) },
                onClick = {
                    open = false
                    onClose()
                },
            )
        }
    }

    if (usageOpen) {
        val panel = remember(usage) {
            usage?.let { UsageSummary.panel(it) } ?: UsageSummary.Panel.NothingYet
        }
        AlertDialog(
            onDismissRequest = { usageOpen = false },
            title = { Text(stringResource(R.string.session_usage_title)) },
            text = {
                when {
                    usageBusy && panel !is UsageSummary.Panel.Rows -> Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        CircularProgressIndicator()
                        Text(stringResource(R.string.session_usage_reading))
                    }

                    // Two different sentences, because they are two different situations: one
                    // means "ask us again later", the other means "this one is not wired up".
                    panel is UsageSummary.Panel.NotReadable ->
                        Text(stringResource(R.string.session_usage_not_readable))

                    panel is UsageSummary.Panel.NothingYet ->
                        Text(stringResource(R.string.session_usage_nothing_yet))

                    else -> SelectionContainer {
                        val rows = (panel as UsageSummary.Panel.Rows).rows
                        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                            for (row in rows) {
                                Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                                    Text(
                                        text = row.label.uppercase(),
                                        style = MaterialTheme.typography.labelSmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    )
                                    Text(
                                        text = row.value,
                                        style = MaterialTheme.typography.bodyMedium,
                                    )
                                }
                            }
                        }
                    }
                }
            },
            confirmButton = {
                TextButton(onClick = { usageOpen = false }) {
                    Text(stringResource(R.string.session_usage_close))
                }
            },
        )
    }
}
