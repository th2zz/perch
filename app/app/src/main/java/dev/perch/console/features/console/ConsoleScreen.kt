package dev.perch.console.features.console

import android.Manifest
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Tab
import androidx.compose.material3.PrimaryTabRow
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
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
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import dev.perch.console.R
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.live.ConnectionStatus
import dev.perch.console.features.hosts.Overview
import dev.perch.console.features.compose.VoiceSetup
import dev.perch.console.features.hosts.WorkspaceDetail
import dev.perch.console.features.manage.NewProjectDialog
import dev.perch.console.features.manage.NewSessionDialog
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.WireValues
import dev.perch.console.features.insight.WeekSummary
import dev.perch.console.features.onboarding.OnboardingScreen
import dev.perch.console.features.onboarding.InviteScreen
import dev.perch.console.features.queue.ComposeReply
import dev.perch.console.features.queue.PerchNotifications
import dev.perch.console.features.queue.QueuePage
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.features.terminal.ShellScreen
import kotlinx.coroutines.delay

/**
 * The one screen the app needs.
 *
 * Two tabs, because the user has exactly two questions: "does anything need me?"
 * and "what is going on?" Everything else the product will grow (the compose
 * screen, the value statistics, the escape hatch) hangs off those two.
 *
 * The tab bar is text-only and the tab set is fixed to two, which is what keeps
 * the "at most three primary actions per screen" criterion true without
 * bookkeeping (spec SC-010).
 */
/**
 * Everything the screen can ask the app to do.
 *
 * The screen takes its state and its callbacks as parameters rather than reaching
 * for a ViewModel, for two reasons. A screen that cannot be constructed without a
 * ViewModel cannot be rendered in a test, which is how this one went unlooked-at
 * for so long. And a list of the actions a screen can take is a useful thing to
 * read: it is the whole of its behaviour in one place.
 */
data class ConsoleActions(
    val refresh: () -> Unit = {},
    val pair: (address: String, token: String, onResult: (String?) -> Unit) -> Unit = { _, _, _ -> },
    val acceptInvite: ((String?) -> Unit) -> Unit = {},
    val dismissInvite: () -> Unit = {},
    val unpair: () -> Unit = {},
    val setWatchInBackground: (Boolean) -> Unit = {},
    val decide: (Decision, DecisionKind, String?) -> Unit = { _, _, _ -> },
    /** Opens a session to read what the agent is saying: pane id, and its computer. */
    val openSession: (String, String) -> Unit = { _, _ -> },
    /**
     * Opens the same session on its terminal side — the escape hatch.
     *
     * A separate action rather than a flag, because the two entries mean different
     * things and a boolean at the call site reads as a detail rather than as a
     * decision. This one is what a card offers when it could not press the dialog.
     */
    val openTerminal: (String, String) -> Unit = { _, _ -> },
    val refreshSession: () -> Unit = {},
    val closeSession: () -> Unit = {},
    /** Tells the state holder whether the session screen is in front of the user. */
    val noteSessionOnScreen: (Boolean) -> Unit = {},
    /** Tells the state holder whether the reader is at the end of the session. */
    val noteSessionFollowing: (Boolean) -> Unit = {},
    /** Asks the computer what the harness itself knows about the open session. */
    val loadSessionUsage: () -> Unit = {},
    /** Opens a real terminal on the open session: the pane's own byte stream. */
    val connectTerminal: () -> dev.perch.console.features.terminal.TerminalConnection? = { null },
    val reportTerminal: (event: String, detail: String) -> Unit = { _, _ -> },
    /**
     * Sends a picture of the terminal screen to the computer.
     *
     * One action, wired to one button: nothing in this app photographs the user's screen unless they press
     * something that says so.
     */
    val sendScreenshot: (png: ByteArray) -> Unit = {},
    val openShell: () -> Unit = {},
    val closeShell: () -> Unit = {},
    val shellConnection: (fresh: Boolean) -> dev.perch.console.features.terminal.ShellConnection? =
        { null },
    val reportShell: (event: String, detail: String) -> Unit = { _, _ -> },
    /**
     * Naming and ending things (2026-10-15).
     *
     * Four actions for the two new doors, because a session and a project are different things to name and
     * very different things to end — closing one session is tidying up, closing a project is not.
     */
    val renameSession: (paneId: String, host: String, label: String) -> Unit = { _, _, _ -> },
    val endSession: (paneId: String, host: String) -> Unit = { _, _ -> },
    val renameProject: (workspaceId: String, host: String, label: String) -> Unit = { _, _, _ -> },
    val endProject: (workspaceId: String, host: String) -> Unit = { _, _ -> },
    val loadHarnesses: (host: String) -> Unit = {},
    /** Lists one directory for the working-folder picker. A read; it creates nothing. */
    val listFolders: suspend (path: String, host: String) -> Result<dev.perch.console.core.herdrclient.FoldersResponse> =
        { _, _ -> Result.failure(IllegalStateException("folder listing is not connected")) },
    val resetCreate: () -> Unit = {},
    val createProject: (
        label: String,
        cwd: String,
        kind: String,
        name: String,
        args: List<String>,
        host: String,
    ) -> Unit = { _, _, _, _, _, _ -> },
    val startSession: (
        workspaceId: String,
        kind: String,
        name: String,
        args: List<String>,
        host: String,
    ) -> Unit = { _, _, _, _, _ -> },
    val approveSession: () -> Unit = {},
    val rejectSession: () -> Unit = {},
    val sendToSession: (String) -> Unit = {},
    val startVoice: () -> Unit = {},
    val stopVoice: () -> Unit = {},
    val discardVoice: () -> Unit = {},
    val clearTranscript: () -> Unit = {},
    val saveVoiceConfig: (apiKey: String, endpoint: String) -> Unit = { _, _ -> },
    val setDecisionViewed: (String) -> Unit = {},
    val dismissProblem: () -> Unit = {},
    val dismissNotice: () -> Unit = {},
    /** Called when the brief "here is what that saved you" line has been read. */
    val clearRecentWin: () -> Unit = {},
    /** Opens the settings screen that fixes one of the silent failures. */
    val openDeviceSetting: (dev.perch.console.core.settings.DeviceAdvice.Fix) -> Unit = {},
    /** Tells the state holder what this phone reports about running and showing things. */
    val noteDeviceRestrictions: (notificationsBlocked: Boolean, backgroundRestricted: Boolean) -> Unit =
        { _, _ -> },
    /** Called once, when the app first shows something the user can act on. */
    val firstUsableScreen: () -> Unit = {},
    /** Makes sure the watching service is running, when the user asked for it. */
    val ensureWatching: () -> Unit = {},
)

/** Wires the screen to the app's state holder. */
@Composable
fun ConsoleScreen(viewModel: ConsoleViewModel) {
    val state by viewModel.state.collectAsStateWithLifecycle()
    ConsoleScreen(
        state = state,
        actions = ConsoleActions(
            refresh = viewModel::refresh,
            pair = { address, token, onResult -> viewModel.pair(address, token, onResult) },
            acceptInvite = { onResult -> viewModel.acceptInvite(onResult) },
            dismissInvite = viewModel::dismissInvite,
            setWatchInBackground = viewModel::setWatchInBackground,
            decide = { decision, kind, text -> viewModel.decide(decision, kind, text) },
            openSession = { paneId, host -> viewModel.openSession(paneId, host) },
            // The card that could not press its dialog opens the *real* terminal now: a live
            // byte stream with a keyboard, which is what answering a dialog actually needs.
            openTerminal = { paneId, host -> viewModel.openSession(paneId, host, inTerminal = true) },
            refreshSession = viewModel::refreshSession,
            closeSession = viewModel::closeSession,
            noteSessionOnScreen = viewModel::noteSessionOnScreen,
            noteSessionFollowing = viewModel::noteSessionFollowing,
            loadSessionUsage = viewModel::loadSessionUsage,
            connectTerminal = viewModel::terminalConnection,
            // Wired, at last. It was declared, passed to the screen, and never connected to the state
            // holder — so `onTerminalReport` was the default no-op and the session screen sent *nothing*
            // for a whole day of debugging: no "the page loaded", no "bytes arrived", no error. The
            // bridge log has zero reports from this screen, ever, which is how I finally noticed.
            reportTerminal = viewModel::reportTerminal,
            sendScreenshot = viewModel::sendScreenshot,
            openShell = viewModel::openShell,
            closeShell = viewModel::closeShell,
            shellConnection = viewModel::shellConnection,
            reportShell = viewModel::reportShell,
            renameSession = { paneId, host, label -> viewModel.renameSession(paneId, host, label) },
            endSession = { paneId, host -> viewModel.closeSession(paneId, host) },
            renameProject = { workspaceId, host, label -> viewModel.renameProject(workspaceId, host, label) },
            endProject = { workspaceId, host -> viewModel.closeProject(workspaceId, host, confirmed = true) },
            loadHarnesses = viewModel::loadHarnesses,
            listFolders = viewModel::listFolders,
            resetCreate = viewModel::resetCreate,
            createProject = { label, cwd, kind, name, args, host ->
                viewModel.createProject(label, cwd, kind, name, args, host)
            },
            startSession = { workspaceId, kind, name, args, host ->
                viewModel.startSession(workspaceId, kind, name, args, host)
            },
            approveSession = viewModel::approveSession,
            rejectSession = viewModel::rejectSession,
            sendToSession = viewModel::sendToSession,
            startVoice = viewModel::startVoiceCapture,
            stopVoice = viewModel::stopVoiceCapture,
            discardVoice = viewModel::discardVoiceCapture,
            clearTranscript = viewModel::clearTranscript,
            saveVoiceConfig = viewModel::saveVoiceConfig,
            dismissProblem = viewModel::dismissProblem,
            dismissNotice = viewModel::dismissNotice,
            clearRecentWin = viewModel::clearRecentWin,
            openDeviceSetting = { fix ->
                val app = viewModel.getApplication<android.app.Application>()
                val intent = when (fix) {
                    dev.perch.console.core.settings.DeviceAdvice.Fix.NotificationSettings ->
                        android.content.Intent(android.provider.Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                            .putExtra(android.provider.Settings.EXTRA_APP_PACKAGE, app.packageName)

                    // The list of apps, not the one-tap "ignore optimisations" dialog:
                    // that dialog needs a permission Google restricts for exactly this
                    // kind of app, and a settings page the user navigates once is
                    // honest about what is being asked.
                    dev.perch.console.core.settings.DeviceAdvice.Fix.BatterySettings ->
                        android.content.Intent(android.provider.Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)
                }
                app.startActivity(intent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
            },
            noteDeviceRestrictions = viewModel::setDeviceRestrictions,
            firstUsableScreen = viewModel::noteFirstUsableScreen,
            ensureWatching = viewModel::ensureWatching,
        ),
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ConsoleScreen(state: ConsoleState, actions: ConsoleActions) {
    var pairingError by remember { mutableStateOf<String?>(null) }
    var tab by remember { mutableIntStateOf(0) }
    var answering by remember { mutableStateOf<Decision?>(null) }
    // Which project the phone is looking at. Local state on purpose: switching
    // here must not reach the computer (see features/hosts/ContextSwitch.kt).
    // Which project is open, as (computer, workspace): the id alone stops being
    // unique the moment the user has two machines.
    var openWorkspace by remember { mutableStateOf<Pair<String, String>?>(null) }
    var newProjectOpen by rememberSaveable { mutableStateOf(false) }
    // Host and workspace ids rather than the workspace itself: the mirror is rebuilt after rotation,
    // and a remembered object would either be stale or fail to survive the activity at all.
    var newSessionIn by rememberSaveable { mutableStateOf<Pair<String, String>?>(null) }
    var createPending by rememberSaveable { mutableStateOf(false) }
    BackHandler(enabled = openWorkspace != null && state.session == null && !state.shellOpen) {
        openWorkspace = null
    }
    val context = LocalContext.current

    val createDialogOpen = newProjectOpen || newSessionIn != null
    LaunchedEffect(state.createBusy, state.createDone, createPending) {
        if (createPending && !state.createBusy && state.createDone) {
            createPending = false
            newProjectOpen = false
            newSessionIn = null
            actions.resetCreate()
        }
    }
    // A dialog restored after process death has none of the harnesses the old process loaded.
    LaunchedEffect(createDialogOpen) {
        if (createDialogOpen && state.harnesses.isEmpty() && !state.harnessesBusy) {
            actions.loadHarnesses(newSessionIn?.first ?: Host.LOCAL)
        }
    }

    // Ask for notification permission once we are actually paired: asking during
    // onboarding, before the app can do anything with it, is how users learn to
    // deny permission requests.
    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission(),
    ) { granted ->
        actions.noteDeviceRestrictions(!granted, deviceFacts(context).backgroundRestricted)
    }

    // Coming back to the screen is the moment to re-ask Android rather than the user:
    // notifications can be switched off in Settings at any time, and the answer can
    // change while this app is in the background.
    val lifecycleOwner = androidx.lifecycle.compose.LocalLifecycleOwner.current
    androidx.compose.runtime.DisposableEffect(lifecycleOwner) {
        val observer = androidx.lifecycle.LifecycleEventObserver { _, event ->
            if (event == androidx.lifecycle.Lifecycle.Event.ON_RESUME) {
                val facts = deviceFacts(context)
                actions.noteDeviceRestrictions(facts.notificationsBlocked, facts.backgroundRestricted)
            }
        }
        lifecycleOwner.lifecycle.addObserver(observer)
        onDispose { lifecycleOwner.lifecycle.removeObserver(observer) }
    }
    // The moment there is something to act on: a queue, or the screen that gets you
    // there. Reported once, from the process's own start, so spec SC-007 becomes a
    // number on the computer instead of a stopwatch in someone's hand.
    LaunchedEffect(state.phase) {
        if (state.phase != ConsoleState.Phase.Loading) actions.firstUsableScreen()
    }

    LaunchedEffect(state.phase, state.watchInBackground) {
        // Only while this screen is visible: Android 12 and up forbid starting a
        // foreground service from the background, and a rejected start would look
        // exactly like the bug this fixes (#52).
        if (state.phase == ConsoleState.Phase.Ready && state.watchInBackground) {
            actions.ensureWatching()
        }
    }
    LaunchedEffect(state.phase, state.connection) {
        if (state.phase != ConsoleState.Phase.Ready) return@LaunchedEffect
        // No SDK check: the minimum supported version is 33, which is when this
        // permission started existing.
        if (!PerchNotifications.allowed(context)) {
            permissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Perch") },
                actions = {
                    // The terminal lives here rather than inside a session, because a shell belongs to the
                    // computer: it is the same one whether or not a session is open, and it is the thing to
                    // reach for when what you want is to run something.
                    if (state.phase == ConsoleState.Phase.Ready) {
                        TextButton(onClick = actions.openShell) {
                            Text(stringResource(R.string.console_terminal))
                        }
                    }
                    if (state.phase == ConsoleState.Phase.Ready) {
                        TextButton(onClick = actions.refresh) { Text("Refresh") }
                    }
                },
            )
        },
    ) { padding ->
        if (state.sessionBusy) {
            androidx.compose.material3.LinearProgressIndicator(
                modifier = Modifier.fillMaxWidth().padding(padding),
            )
        }
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(padding),
        ) {
            when (state.phase) {
                ConsoleState.Phase.Loading -> Text(
                    text = "Looking for your computer…",
                    modifier = Modifier.padding(24.dp),
                )

                ConsoleState.Phase.NeedsPairing -> {
                    val invite = state.invite
                    if (invite != null) {
                        // A link was tapped: everything needed is already here, so
                        // the only thing left is the user's consent.
                        InviteScreen(
                            invite = invite,
                            busy = false,
                            error = pairingError,
                            onAccept = {
                                actions.acceptInvite { error -> pairingError = error }
                            },
                            onUseManualEntry = {
                                pairingError = null
                                actions.dismissInvite()
                            },
                        )
                    } else {
                        OnboardingScreen(
                            busy = false,
                            error = pairingError,
                            onPair = { address, token ->
                                actions.pair(address, token) { error -> pairingError = error }
                            },
                        )
                    }
                }

                ConsoleState.Phase.Ready -> {
                    // A refused action and an unreachable computer are both
                    // "something happened"; both come with a fix, and both stay
                    // until the user dismisses them.
                    state.problem?.let { problem ->
                        ProblemBanner(
                            message = problem.message,
                            fix = problem.fix,
                            onDismiss = actions.dismissProblem,
                        )
                    }
                    state.notice?.let { notice ->
                        LaunchedEffect(notice) {
                            delay(8_000)
                            actions.dismissNotice()
                        }
                        Text(
                            text = notice,
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.primary,
                            modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                        )
                    }

                    PrimaryTabRow(selectedTabIndex = tab) {
                        Tab(
                            selected = tab == 0,
                            onClick = { tab = 0 },
                            text = {
                                val count = state.waiting.size
                                Text(
                                    if (count > 0) {
                                        stringResource(R.string.tab_decisions) + " ($count)"
                                    } else {
                                        stringResource(R.string.tab_decisions)
                                    },
                                )
                            },
                        )
                        Tab(
                            selected = tab == 1,
                            onClick = { tab = 1 },
                            text = { Text(stringResource(R.string.tab_projects)) },
                        )
                    }

                    when (tab) {
                        0 -> Column(Modifier.fillMaxSize()) {
                            ConnectionLine(state)
                            // Settings that make the product quietly stop working: a
                            // phone that cannot notify, and a phone that stops
                            // background apps by policy. Shown with the fix, never
                            // dismissed on a timer — they stay true until the user
                            // changes them (spec FR-015). Which ones appear is decided
                            // by DeviceAdvice, which is a pure function and has tests.
                            for (advice in deviceAdvice(state)) {
                                Row(
                                    modifier = Modifier
                                        .fillMaxWidth()
                                        .padding(horizontal = 16.dp),
                                    verticalAlignment = Alignment.CenterVertically,
                                ) {
                                    Text(
                                        text = stringResource(advice.message),
                                        style = MaterialTheme.typography.labelMedium,
                                        color = MaterialTheme.colorScheme.error,
                                        modifier = Modifier.weight(1f),
                                    )
                                    TextButton(onClick = { actions.openDeviceSetting(advice.fix) }) {
                                        Text(stringResource(advice.action))
                                    }
                                }
                            }
                            // The confirmation appears where the tap happened and
                            // leaves on its own: this is an acknowledgement, not a
                            // notification the user has to dismiss.
                            state.recentWin?.let { win ->
                                LaunchedEffect(win) {
                                    delay(6_000)
                                    actions.clearRecentWin()
                                }
                                Text(
                                    text = win,
                                    style = MaterialTheme.typography.labelMedium,
                                    color = MaterialTheme.colorScheme.primary,
                                    modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
                                )
                            }
                            QueuePage(
                                waiting = state.waiting,
                                busyDecisionId = state.busyDecisionId,
                                onDecide = { decision, kind -> actions.decide(decision, kind, null) },
                                onAnswer = { answering = it },
                                onOpenSession = { actions.openTerminal(it.paneId, it.hostId) },
                                // The week's totals sit under the queue rather than
                                // on a tab of their own: they are an answer to
                                // "was this worth it", which is a question the
                                // queue's own screen raises.
                                modifier = Modifier.weight(1f),
                            )
                            WeekSummary(stats = state.week)
                        }
                        else -> Column(Modifier.fillMaxSize()) {
                            val selected = openWorkspace?.let { key ->
                                state.mirror.workspace(key.first, key.second)
                            }

                            // The phone's own settings sit alongside the project list,
                            // because both answer "how does this phone relate to the
                            // computers". They are hidden once the user opens a
                            // project: that is a drill-down, and leaving a settings
                            // block above it mixes two levels of the same screen.
                            if (selected == null) {
                                WatchToggle(
                                    enabled = state.watchInBackground,
                                    onChange = actions.setWatchInBackground,
                                )
                                VoiceSetup(
                                    configured = state.voiceConfigured,
                                    onSave = actions.saveVoiceConfig,
                                )
                                // Every computer this phone can see, with what it is
                                // doing. The old version said "other machines are
                                // saved but not read"; now they are read, and a
                                // computer that did not answer says why (FR-015),
                                // so its projects do not look like projects that
                                // vanished.
                                if (state.mirror.showHostNames) {
                                    Text(
                                        text = stringResource(R.string.computers_heading),
                                        style = MaterialTheme.typography.labelMedium,
                                        color = MaterialTheme.colorScheme.primary,
                                        modifier = Modifier.padding(horizontal = 16.dp),
                                    )
                                    state.mirror.hosts.forEach { host ->
                                        Text(
                                            text = hostLine(state.mirror, host),
                                            style = MaterialTheme.typography.labelSmall,
                                            color = if (host.status == WireValues.HostStatus.UNREACHABLE) {
                                                MaterialTheme.colorScheme.error
                                            } else {
                                                MaterialTheme.colorScheme.onSurfaceVariant
                                            },
                                            modifier = Modifier.padding(horizontal = 16.dp),
                                        )
                                    }
                                }
                            }

                            if (selected != null) {
                                WorkspaceDetail(
                                    workspace = selected,
                                    mirror = state.mirror,
                                    onBack = { openWorkspace = null },
                                    onOpenSession = { paneId, host -> actions.openSession(paneId, host) },
                                    onNewSession = {
                                        actions.dismissProblem()
                                        actions.resetCreate()
                                        newSessionIn = selected.hostId to selected.workspaceId
                                        actions.loadHarnesses(selected.hostId)
                                    },
                                    onRename = { label ->
                                        actions.renameProject(selected.workspaceId, selected.hostId, label)
                                    },
                                    onEndProject = {
                                        actions.endProject(selected.workspaceId, selected.hostId)
                                    },
                                    onEndSession = actions.endSession,
                                )
                            } else {
                                Overview(
                                    mirror = state.mirror,
                                    onOpenWorkspace = { openWorkspace = it.hostId to it.workspaceId },
                                    onNewProject = {
                                        actions.dismissProblem()
                                        actions.resetCreate()
                                        newProjectOpen = true
                                        actions.loadHarnesses(Host.LOCAL)
                                    },
                                )
                            }
                        }
                    }
                }
            }
        }
    }

    answering?.let { decision ->
        ComposeReply(
            decision = decision,
            busy = state.busyDecisionId == decision.id,
            voiceConfigured = state.voiceConfigured,
            voiceRecording = state.voiceRecording,
            voiceTranscribing = state.voiceTranscribing,
            transcript = state.transcript,
            onVoiceStart = actions.startVoice,
            onVoiceStop = actions.stopVoice,
            onVoiceDiscard = actions.discardVoice,
            onSend = { text ->
                actions.decide(decision, dev.perch.console.core.herdrclient.DecisionKind.ANSWER, text)
                actions.clearTranscript()
                answering = null
            },
            onDismiss = { answering = null },
        )
    }

    if (newProjectOpen) {
        NewProjectDialog(
            harnesses = state.harnesses,
            harnessesBusy = state.harnessesBusy,
            busy = state.createBusy,
            homeFolder = state.homeFolder,
            recentFolders = state.recentFolders,
            problem = state.problem?.message,
            onDismiss = { newProjectOpen = false },
            onListFolders = { path -> actions.listFolders(path, Host.LOCAL) },
            onConfirm = { label, cwd, kind, name, args ->
                createPending = true
                actions.createProject(label, cwd, kind, name, args, Host.LOCAL)
            },
        )
    }

    newSessionIn?.let { key ->
        val workspace = state.mirror.workspace(key.first, key.second)
        NewSessionDialog(
            projectLabel = workspace?.label?.takeIf { it.isNotBlank() } ?: key.second,
            harnesses = state.harnesses,
            harnessesBusy = state.harnessesBusy,
            busy = state.createBusy,
            problem = state.problem?.message,
            onDismiss = { if (!state.createBusy) newSessionIn = null },
            onConfirm = { kind, name, args ->
                createPending = true
                actions.startSession(key.second, kind, name, args, key.first)
            },
        )
    }

    if (state.shellOpen) {
        ShellScreen(
            hostLabel = state.mirror.hostLabel(Host.LOCAL),
            connect = actions.shellConnection,
            onClose = actions.closeShell,
            onReport = actions.reportShell,
            onSendScreenshot = actions.sendScreenshot,
        )
        return
    }

    state.session?.let { session ->
        SessionScreen(
            session = session,
            busy = state.sessionBusy,
            onApprove = actions.approveSession,
            onReject = actions.rejectSession,
            onSend = actions.sendToSession,
            onRefresh = actions.refreshSession,
            onClose = actions.closeSession,
            onVisible = actions.noteSessionOnScreen,
            onFollowing = actions.noteSessionFollowing,
            usage = state.sessionUsage,
            usageBusy = state.sessionUsageBusy,
            onLoadUsage = actions.loadSessionUsage,
            conversation = state.conversation,
            connectTerminal = actions.connectTerminal,
            onTerminalReport = actions.reportTerminal,
            onSendScreenshot = actions.sendScreenshot,
            onRename = { label -> actions.renameSession(session.paneId, session.hostId, label) },
            onEndSession = { actions.endSession(session.paneId, session.hostId) },
            // "Last one in the project" is a fact about the computer's list, not about this session, so it is
            // answered here where the whole list is in hand (measured: herdr closes the project when its last
            // pane goes, and the dialog has to say so before somebody taps).
            lastInProject = state.mirror.panesOf(session.hostId, session.workspaceId).size <= 1,
            // A session opened from a card that could not press its dialog goes straight to the
            // terminal: that is where the keys are.
            openTerminalOnOpen = state.sessionInTerminal,
            terminalOnly = state.terminalOnlySessions,
        )
    }
}

/**
 * A one-line statement of how the phone and the computer are getting along.
 *
 * Worth the space: without it, "no decisions waiting" and "not connected" look
 * identical, and the user would trust a screen that has quietly stopped being
 * informed.
 */
@Composable
private fun ConnectionLine(state: ConsoleState) {
    val text = when {
        // Three statements about the connection, and nothing about the queue: what
        // is waiting is the queue's own business, and only its own.
        state.offline || state.connection == ConnectionStatus.Retrying ->
            stringResource(R.string.connection_retrying)
        state.connection == ConnectionStatus.Live -> stringResource(R.string.connection_live)
        else -> stringResource(R.string.watching_connecting)
    }
    Text(
        text = text,
        style = MaterialTheme.typography.labelSmall,
        color = if (state.offline) {
            MaterialTheme.colorScheme.error
        } else {
            MaterialTheme.colorScheme.onSurfaceVariant
        },
        modifier = Modifier.padding(horizontal = 16.dp, vertical = 4.dp),
    )
}

@Composable
private fun ProblemBanner(message: String, fix: String, onDismiss: () -> Unit) {
    Card(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.errorContainer,
        ),
    ) {
        Column(Modifier.padding(12.dp)) {
            Text(text = message, style = MaterialTheme.typography.bodyMedium)
            if (fix.isNotBlank()) {
                Text(text = fix, style = MaterialTheme.typography.bodySmall)
            }
            Row(horizontalArrangement = Arrangement.End, modifier = Modifier.fillMaxWidth()) {
                TextButton(onClick = onDismiss) { Text("Got it") }
            }
        }
    }
}

@Composable
private fun WatchToggle(enabled: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Column(Modifier.weight(1f)) {
            Text(
                text = stringResource(R.string.keep_watching),
                style = MaterialTheme.typography.bodyMedium,
            )
            Text(
                text = stringResource(R.string.keep_watching_detail),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
        Switch(checked = enabled, onCheckedChange = onChange)
    }
}

/**
 * One computer, as a single line: its name and whether it answered.
 *
 * A computer that could not be read says so here and again in the project list,
 * because "my laptop's projects are missing" is exactly the confusion this line
 * exists to prevent (spec FR-015).
 */
@Composable
private fun hostLine(mirror: Mirror, host: Host): String = when {
    host.status == WireValues.HostStatus.UNREACHABLE -> "${mirror.hostLabel(host.hostId)} — " + stringResource(R.string.host_unreachable_title)
    host.status == "disabled" -> "${mirror.hostLabel(host.hostId)} — " + stringResource(R.string.host_disabled)
    host.hostId == Host.LOCAL -> "${mirror.hostLabel(host.hostId)} — " + stringResource(R.string.host_this_computer)
    else -> "${mirror.hostLabel(host.hostId)} — " + stringResource(R.string.host_connected)
}

/** What this phone reports about notifying and about running in the background. */
private data class DeviceFacts(val notificationsBlocked: Boolean, val backgroundRestricted: Boolean)

/**
 * Reads the two facts from the phone itself.
 *
 * `isIgnoringBatteryOptimizations` is the closest thing Android offers to "will you
 * let my background service live": false can mean Doze will stop it, and on several
 * manufacturers it means an app manager will kill it much sooner than that.
 */
private fun deviceFacts(context: android.content.Context): DeviceFacts {
    val power = context.getSystemService(android.content.Context.POWER_SERVICE) as? android.os.PowerManager
    return DeviceFacts(
        notificationsBlocked = !PerchNotifications.allowed(context),
        backgroundRestricted = power?.isIgnoringBatteryOptimizations(context.packageName) == false,
    )
}

/** The advice this screen shows, from the state and the same rules the tests pin. */
private fun deviceAdvice(state: ConsoleState) = dev.perch.console.core.settings.DeviceAdvice.forFacts(
    paired = state.phase == ConsoleState.Phase.Ready,
    watchingInBackground = state.watchInBackground,
    notificationsBlocked = state.notificationsBlocked,
    backgroundRestricted = state.backgroundRestricted,
)
