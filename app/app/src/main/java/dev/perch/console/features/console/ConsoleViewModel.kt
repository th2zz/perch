package dev.perch.console.features.console

import android.app.Application
import android.os.Process
import android.os.SystemClock
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.perch.console.AppContainer
import dev.perch.console.BuildConfig
import dev.perch.console.PerchApp
import dev.perch.console.R
import dev.perch.console.core.cache.CachedState
import dev.perch.console.core.auth.PairInvite
import dev.perch.console.core.herdrclient.AgentStates
import dev.perch.console.core.herdrclient.ApiCompatibility
import dev.perch.console.core.herdrclient.BridgeClient
import dev.perch.console.core.herdrclient.BridgeException
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.herdrclient.FoldersResponse
import dev.perch.console.core.herdrclient.Harness
import dev.perch.console.core.herdrclient.ManageResult
import dev.perch.console.core.herdrclient.ManageTarget
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.SHELL_PANE_ID
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.Transcript
import dev.perch.console.core.herdrclient.ValueStats
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.core.herdrclient.WireValues
import dev.perch.console.core.live.BridgeStream
import dev.perch.console.core.live.BridgeEventTypes
import dev.perch.console.core.live.ConnectionStatus
import dev.perch.console.core.errors.FailureKind
import dev.perch.console.core.errors.Failures
import dev.perch.console.core.outbox.QueuedReply
import dev.perch.console.core.startup.StartupMeasurement
import dev.perch.console.core.voice.Transcriber
import dev.perch.console.core.voice.Transcription
import dev.perch.console.core.voice.VoiceConfig
import dev.perch.console.features.compose.VoiceRecorder
import dev.perch.console.features.queue.BridgeWatchService
import dev.perch.console.features.queue.DecisionExecutor
import dev.perch.console.features.queue.DecisionOutcome
import dev.perch.console.features.queue.Disposition
import dev.perch.console.features.queue.PerchNotifications
import dev.perch.console.features.queue.dispositionOf
import dev.perch.console.features.terminal.LiveSession
import dev.perch.console.features.terminal.ShellConnection
import dev.perch.console.features.terminal.TerminalConnection
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock

/**
 * Everything the screen needs, in one immutable value.
 *
 * A single state object rather than several flows: the screen either has a
 * coherent view of the computer or it does not, and splitting the state would let
 * the UI render an impossible combination — the queue from now, the project list
 * from ten minutes ago.
 */
data class ConsoleState(
    val phase: Phase = Phase.Loading,
    val mirror: Mirror = Mirror(),
    val decisions: List<Decision> = emptyList(),
    /** True when what is shown came from the cache because the computer is unreachable. */
    val offline: Boolean = false,
    /** Set when something went wrong; always carries a next step. */
    val problem: Problem? = null,
    val busyDecisionId: String? = null,
    val connection: ConnectionStatus = ConnectionStatus.Unpaired,
    val watchInBackground: Boolean = true,
    val terminalOnlySessions: Boolean = false,
    /**
     * True when Android notifications are switched off for this app.
     *
     * It is in the state rather than inferred in the UI because it is the one
     * condition that makes the product silently useless: the agent waits, the
     * phone says nothing, and every screen still looks healthy. Reported from the
     * phone's own permission state, so it is true whether the user denied the
     * prompt, or turned notifications off in Settings months later.
     */
    val notificationsBlocked: Boolean = false,
    /**
     * True when this phone restricts background apps, which some manufacturers do
     * whatever Android's own setting says. Reported rather than discovered later,
     * because the symptom — notifications that stop a few minutes after the screen
     * goes off — is indistinguishable from the product being broken.
     */
    val backgroundRestricted: Boolean = false,
    /** This week's totals, as the computer reported them. */
    val week: ValueStats = ValueStats(),
    /**
     * The line shown right after a decision lands.
     *
     * It exists because attribution has to happen at the moment it is true:
     * a weekly total is a statistic, whereas "that just saved you a walk to the
     * computer" is a fact the user can feel. Cleared by the UI after a moment.
     */
    val recentWin: String? = null,
    /** Plain statements that are not failures, e.g. "saved, will send later". */
    val notice: String? = null,
    /** How many replies are waiting for the computer to come back. */
    val queuedReplies: Int = 0,
    /**
     * The session the user opened to look at, when they have opened one.
     *
     * This is the escape hatch: what is on screen, read-only, plus the right to
     * type into it. It exists because refusing to guess is only honest if there is
     * something behind the refusal.
     */
    val session: SessionView? = null,
    val conversation: Transcript? = null,
    /** What the harness itself knows about the open session; null until asked for. */
    val sessionUsage: dev.perch.console.core.herdrclient.UsageFacts? = null,
    /** True while the computer is being asked for the usage panel. */
    val sessionUsageBusy: Boolean = false,
    val sessionInTerminal: Boolean = false,
    val sessionBusy: Boolean = false,
    val harnesses: List<Harness> = emptyList(),
    val harnessesBusy: Boolean = false,
    /** The computer's home folder, learned from the harness list; "Use home" needs to name it. */
    val homeFolder: String = "",
    /** Working folders this phone has used, most recent first, for the picker's recent row. */
    val recentFolders: List<String> = emptyList(),
    val createBusy: Boolean = false,
    /** True once the last creation returned, so the dialog can close on success and stay on failure. */
    val createDone: Boolean = false,
    /**
     * Whether the terminal — the shell on the computer — is in front of the user.
     *
     * Its own field rather than part of the session, because it is not about a pane: a shell belongs to
     * the computer, can be opened with or without a session, and outlives the session screen being closed.
     */
    val shellOpen: Boolean = false,
    /**
     * Whether that shell was just replaced on purpose ("new terminal").
     *
     * Kept so the screen can say which one it is showing: a fresh shell and a reattached one look identical
     * on a phone, and "did my `cd` survive" deserves a number rather than a guess.
     */
    val shellFresh: Boolean = false,
    /** True when the user has set up their own transcription key. */
    val voiceConfigured: Boolean = false,
    val voiceRecording: Boolean = false,
    val voiceTranscribing: Boolean = false,
    /** Text that arrived from a recording, waiting to be edited or sent. */
    val transcript: String? = null,
    /**
     * An invitation that arrived by tapping a pairing link.
     *
     * Held in state rather than applied immediately so the user gets to see
     * which computer they are about to trust. Auto-connecting from a link would
     * be faster and worse: it spends a security decision on the user's behalf
     * that they did not make.
     */
    val invite: PairInvite? = null,
    val lastUpdatedMillis: Long = 0L,
) {
    enum class Phase { Loading, NeedsPairing, Ready }

    data class Problem(val message: String, val fix: String)

    val waiting: List<Decision> get() = decisions.filter { it.state == WireValues.DecisionState.PENDING }

    val orderedWorkspaces: List<Workspace> get() = mirror.workspaces.sortedBy { it.number }

    /** How many agents are currently waiting on a person. */
    val waitingAgents: Int get() = mirror.panes.count { AgentStates.needsDecision(it.agentStatus) }
}

class ConsoleViewModel(application: Application) : AndroidViewModel(application) {

    private val container: AppContainer = (application as PerchApp).container

    private val _state = MutableStateFlow(
        ConsoleState(
            watchInBackground = container.settings.watchInBackground,
            terminalOnlySessions = BuildConfig.SESSION_SCREEN_ONLY,
            queuedReplies = container.outbox.size(),
            voiceConfigured = container.voiceSettings.load() != null,
            recentFolders = container.settings.recentFolders,
        ),
    )
    val state: StateFlow<ConsoleState> = _state.asStateFlow()

    /**
     * True once the phone has told the computer how long it took to start.
     *
     * Guarded because the first frame can settle more than once as state arrives;
     * reporting every time would fill the measurement with the settling rather
     * than the start.
     */
    private var startupReported = false

    /**
     * Uptime when this launch began, or 0 when nothing recorded it.
     *
     * See [noteLaunchStart] and [StartupMeasurement]: without it, the reported
     * startup figure falls back to the process start, which is right only when the
     * process really was started by this launch.
     */
    private var launchStartedAtMillis = 0L

    /**
     * One key press at a time, in the order they were tapped.
     *
     * Serialised rather than guarded: a person walking a menu with ↓ taps faster
     * than a round trip, and dropping those taps would read as a broken button.
     */
    private val keyPresses = Mutex()

    /**
     * The loop that keeps an open session up to date, and whether its screen is
     * actually in front of the user.
     *
     * Both halves are needed. The job alone would keep polling after the user left
     * the session screen open and switched to another app — the phone would spend
     * the afternoon asking the laptop for a screen nobody is looking at, which is
     * exactly the kind of cost this product must not have (the watching service is
     * the only part meant to run in the background, and it does not poll).
     *
     * [LiveSession] holds the rules; this holds the lifetime.
     */
    private var sessionPoll: Job? = null
    private var sessionOnScreen = false

    private var sessionFollowing = true

    private val recorder by lazy { VoiceRecorder(getApplication()) }
    private val transcriber = Transcriber()

    /**
     * True once the event collectors are running.
     *
     * They used to be started from both `init` and `pair`, so pairing after the
     * first screen — the normal path — left six collectors on one stream and made
     * every event cost a multiple of a full refresh. Nothing about the symptoms
     * would have pointed here: the app would simply have felt expensive.
     */
    private var observing = false

    /**
     * One refresh at a time, with at most one more queued behind it.
     *
     * A refresh is not free: the bridge reads the whole session and asks herdr for
     * its machine list, which spawns a process. Agents change state in bursts, so
     * reacting to every event individually means several of those per second —
     * visible as churn on the user's machine and as battery on their phone. Collapsing
     * a burst into "one now, one after" keeps the screen as current as the events
     * demanded without paying for each one.
     */
    private var refreshing = false
    private var refreshAgain = false

    init {
        // Show the last known state immediately: a cold start that waits for the
        // network would violate "under two seconds to the queue".
        container.cachedState()?.let { cached ->
            _state.value = _state.value.copy(
                mirror = cached.mirror,
                decisions = cached.decisions,
                lastUpdatedMillis = cached.savedAtMillis,
            )
        }
        observeLive()
        refresh()
    }

    /**
     * Which contract the computer speaks, as it last reported.
     *
     * Held rather than asked on every refresh: it can only change when the bridge is
     * updated, which cannot happen without the connection dropping, and the drop
     * clears this. Asking on every read would put a second request on the path the
     * user feels.
     */
    private var compatibility: ApiCompatibility? = null

    /** Re-reads everything from the computer. Safe to call at any time. */
    fun refresh() {
        val client = container.client()
        if (client == null) {
            _state.value = _state.value.copy(phase = ConsoleState.Phase.NeedsPairing)
            return
        }
        if (compatibility == null) {
            viewModelScope.launch { checkCompatibility(client) }
        }
        // Collapse a burst of events into one read in flight plus one queued
        // follow-up. See `refreshing` for why this matters more than it looks.
        if (refreshing) {
            refreshAgain = true
            return
        }
        refreshing = true
        viewModelScope.launch {
            try {
                val snapshot = client.snapshot()
                apply(snapshot.mirror, snapshot.decisions, offline = false, problem = null)
                refreshWeek(client)
                // The connection is proven, so anything held for it can go now.
                flushOutbox()
            } catch (e: BridgeException) {
                fallBackToCache(e)
            } catch (e: Exception) {
                fallBackToCache(
                    BridgeException(
                        message = e.message ?: "could not reach your computer",
                        fix = "check that the computer is awake and Perch is running there",
                    ),
                )
            } finally {
                refreshing = false
                if (refreshAgain) {
                    // Whatever arrived while this read was in flight wanted a newer
                    // answer than this one, so it gets exactly one more read.
                    refreshAgain = false
                    refresh()
                }
            }
        }
    }

    /**
     * Asks the computer what contract it speaks, and says so if the two disagree.
     *
     * The spec's edge case is explicit about this: a version mismatch must be told,
     * not turned into an error state. Being told is also the difference between a
     * user updating the right half and a user concluding the app is broken.
     */
    private suspend fun checkCompatibility(client: BridgeClient) {
        val response = try {
            client.health()
        } catch (_: Exception) {
            // The screen already reports unreachability from the refresh path; a
            // second complaint about the same silence is noise.
            return
        }
        val verdict = ApiCompatibility.of(response.apiVersion)
        compatibility = verdict
        if (!verdict.isMismatch) return
        val app = getApplication<Application>()
        _state.value = _state.value.copy(
            problem = ConsoleState.Problem(
                message = app.getString(verdict.messageResource()!!),
                fix = app.getString(verdict.fixResource()!!),
            ),
        )
    }

    /** Reads the weekly totals. A failure here must not disturb the main screen. */
    private suspend fun refreshWeek(client: BridgeClient) {
        runCatching { client.value() }
            .onSuccess { _state.value = _state.value.copy(week = it.week) }
    }

    private fun apply(
        mirror: Mirror,
        decisions: List<Decision>,
        offline: Boolean,
        problem: ConsoleState.Problem?,
    ) {
        _state.value = _state.value.copy(
            phase = ConsoleState.Phase.Ready,
            mirror = mirror,
            decisions = decisions,
            offline = offline,
            problem = problem,
            lastUpdatedMillis = System.currentTimeMillis(),
        )
        container.cacheState(
            CachedState(
                mirror = mirror,
                decisions = decisions,
                savedAtMillis = System.currentTimeMillis(),
            ),
        )
    }

    /**
     * Offline is a known boundary, not a defect (spec Assumptions): the computer
     * may be asleep. The app keeps showing the last known queue and says so,
     * rather than emptying the screen or pretending the state is current.
     */
    private fun fallBackToCache(error: BridgeException) {
        val cached = container.cachedState()
        _state.value = _state.value.copy(
            phase = ConsoleState.Phase.Ready,
            mirror = cached?.mirror ?: _state.value.mirror,
            decisions = cached?.decisions ?: _state.value.decisions,
            offline = true,
            problem = ConsoleState.Problem(
                message = error.message,
                fix = error.fix.ifBlank {
                    "check that the computer is awake and connected to the same network"
                },
            ),
        )
    }

    private fun observeLive() {
        if (observing) return
        if (container.client() == null) return
        observing = true
        // Idempotent: the watching service may already own the connection, in
        // which case this joins the existing one rather than opening a second.
        BridgeStream.start { container.client() }

        viewModelScope.launch {
            BridgeStream.status.collect { status ->
                _state.value = _state.value.copy(connection = status)
            }
        }
        viewModelScope.launch {
            BridgeStream.events.collect { event ->
                // The set of types lives in BridgeEventTypes, pinned to the bridge's
                // own list by a shared contract file: this used to be a `when` with
                // bare literals and no fallback, so a rename on the bridge would have
                // been a silent "nothing needs you" on the phone.
                if (!BridgeEventTypes.movesTheQueue(event.type)) {
                    // The heartbeat lands here, deliberately: it says the socket is
                    // alive, not that anything moved, and re-reading on it would make
                    // a quiet connection cost a request every thirty seconds.
                    return@collect
                }

                if (event.type == BridgeEventTypes.RESOLVED) {
                    // A card that simply disappears is a card the user will wonder
                    // about. The computer already wrote an honest sentence for this
                    // case ("the agent resumed before you acted — most likely handled
                    // on the computer"), so it is shown rather than paraphrased. Only
                    // for requests resolved elsewhere: the ones the user answered
                    // themselves already get their own line.
                    val decision = event.decision
                    if (decision != null && decision.state == WireValues.DecisionState.HANDLED_ELSEWHERE) {
                        _state.value = _state.value.copy(
                            notice = decision.stateReason
                                ?.takeIf { it.isNotBlank() }
                                ?: getApplication<Application>()
                                    .getString(R.string.request_resolved_elsewhere),
                        )
                    }
                }
                // Any of these means the queue may have moved. Re-read rather than
                // trust the payload: a stream that has silently fallen behind is
                // exactly the failure that would make the app lie about what needs
                // the user.
                refresh()
            }
        }
        // A fresh connection is a moment worth re-reading: the phone may have been
        // away for hours, and events during that time were never delivered.
        viewModelScope.launch {
            BridgeStream.status.collect { status ->
                if (status == ConnectionStatus.Live) {
                    // A reconnect is exactly when the computer may have been updated,
                    // so what it said about its contract is asked again.
                    compatibility = null
                    refresh()
                }
            }
        }
    }

    /**
     * Delivers replies that were written while the computer was unreachable.
     *
     * Runs only once a connection has been confirmed, and stops at the first
     * still-unreachable entry rather than grinding through the list: if one did
     * not get through, the others will not either.
     */
    private suspend fun flushOutbox() {
        val queued = container.outbox.list()
        if (queued.isEmpty()) {
            if (_state.value.queuedReplies != 0) {
                _state.value = _state.value.copy(queuedReplies = 0)
            }
            return
        }

        var delivered = 0
        var refusal: DecisionOutcome.Refused? = null
        for (entry in queued) {
            val kind = DecisionKind.entries.firstOrNull { it.wire == entry.kind }
            if (kind == null) {
                container.outbox.remove(entry.decisionId)
                continue
            }
            val outcome = DecisionExecutor.run(
                context = getApplication(),
                decisionId = entry.decisionId,
                paneId = entry.paneId,
                kind = kind,
                text = entry.text,
                host = entry.host,
            )
            // What happens to a queued reply is decided by Disposition, which is a
            // pure function with tests: delivered leaves, a refusal leaves (the
            // computer has already answered), and silence stays and stops the round
            // — the rest of the list is behind the same silence.
            when (dispositionOf(outcome)) {
                Disposition.Delivered -> {
                    container.outbox.remove(entry.decisionId)
                    delivered++
                }
                Disposition.Settled -> {
                    container.outbox.remove(entry.decisionId)
                    if (refusal == null && outcome is DecisionOutcome.Refused) refusal = outcome
                }
                Disposition.KeepForLater -> {
                    container.outbox.recordFailure(
                        entry.decisionId,
                        (outcome as? DecisionOutcome.Unreachable)?.reason ?: "no answer",
                    )
                    break
                }
            }
        }

        val remaining = container.outbox.size()
        _state.value = _state.value.copy(
            queuedReplies = remaining,
            notice = when {
                // The count picks the wording. Hand-rolling this is how a product
                // ends up saying "1 replies" in a language nobody tested.
                delivered > 0 -> getApplication<Application>().resources.getQuantityString(
                    R.plurals.sent_waiting_replies,
                    delivered,
                    delivered,
                )
                else -> _state.value.notice
            },
            problem = refusal?.let { ConsoleState.Problem(it.message, it.fix) } ?: _state.value.problem,
        )
    }

    /** Pairs with the computer after verifying the address and token work. */
    fun pair(address: String, token: String, onResult: (String?) -> Unit) {
        // A pairing link is accepted in either field. The designed path is to tap
        // the link and never see this screen, but a link that arrives in an app
        // that will not open it — or gets sent as text — must not leave the user
        // retyping an address and a token by hand. The link already contains both;
        // recognising it here is the difference between a fallback and a dead end.
        val pasted = dev.perch.console.core.auth.PairLink.fromFields(address, token)
        val effectiveAddress = pasted?.address ?: address
        val effectiveToken = pasted?.token ?: token

        val url = BridgeClient.parseAddress(effectiveAddress)
        if (url == null) {
            onResult(getApplication<Application>().getString(R.string.pair_bad_address))
            return
        }
        viewModelScope.launch {
            val candidate = BridgeClient(url, effectiveToken.trim())
            try {
                val health = candidate.health()
                // Refused here rather than discovered later as a 400 on the first
                // tap: the user is standing in front of the setup screen, which is
                // the best possible moment to be told one of the two halves is old.
                val verdict = ApiCompatibility.of(health.apiVersion)
                if (verdict.isMismatch) {
                    onResult(
                        getApplication<Application>().getString(verdict.messageResource()!!) + " " +
                            getApplication<Application>().getString(verdict.fixResource()!!),
                    )
                    return@launch
                }
                // Storing the token has its own failure, and it is not the computer's
                // fault: the token is encrypted with a key in the phone's keystore, and
                // if that is unavailable (a broken or unusual device), the honest
                // report is about this phone. The catch below used to turn it into
                // "Could not reach that address" — blaming a network that had just
                // answered.
                try {
                    container.pairingStore.save(
                        dev.perch.console.core.auth.Pairing(effectiveAddress.trim(), effectiveToken.trim()),
                    )
                } catch (e: Exception) {
                    onResult(getApplication<Application>().getString(R.string.pair_store_failed))
                    return@launch
                }
                _state.value = _state.value.copy(phase = ConsoleState.Phase.Ready)
                onResult(null)
                setWatchInBackground(container.settings.watchInBackground)
                observeLive()
                refresh()
            } catch (e: Exception) {
                onResult(
                    (e as? BridgeException)?.message
                        ?: "Could not reach that address. Check the computer is awake and Perch is running.",
                )
            }
        }
    }

    fun unpair() {
        BridgeWatchService.stop(getApplication())
        BridgeStream.stop()
        container.pairingStore.clear()
        // Forgets the mirror *and* the outbox. The outbox is the load-bearing half:
        // it holds replies the user wrote while the computer was asleep, and a
        // reply written for this computer must not be delivered to the next one
        // the user pairs with. See AppContainer.forgetPairing.
        container.forgetPairing()
        // And what we showed about it: the decision cards in the shade are answers
        // to this computer's agents, and they must not outlive the pairing.
        PerchNotifications.cancelAll(getApplication())
        _state.value = ConsoleState(phase = ConsoleState.Phase.NeedsPairing)
    }

    /** A pairing link was tapped; show it and wait for one confirmation. */
    fun offerInvite(invite: PairInvite) {
        _state.value = _state.value.copy(invite = invite)
    }

    /** The user confirmed the invite; connect with it. */
    fun acceptInvite(onResult: (String?) -> Unit) {
        val invite = _state.value.invite ?: return
        pair(invite.address, invite.token) { error ->
            if (error == null) {
                _state.value = _state.value.copy(invite = null)
            }
            onResult(error)
        }
    }

    /** The user would rather type it, or the link was not for this phone. */
    fun dismissInvite() {
        _state.value = _state.value.copy(invite = null)
    }

    /**
     * Turns the background watch on or off.
     *
     * Kept as an explicit user choice because the cost is visible: one permanent
     * quiet notification and a little battery. The alternative — deciding for
     * them — would either surprise them with a notification they did not expect
     * or silently downgrade the product to "only tells you while it is open".
     */
    /**
     * Makes sure the watching service is running, when the user asked for it.
     *
     * Called from the console screen once it is showing a connected computer. It
     * exists because of what the first real trial found: Android's "force stop"
     * leaves the service stopped, and the app only ever started it on pair, on the
     * toggle and at boot — so after a force stop the user could open the app, see a
     * switch that said "on", and have nothing watching at all (#52). Calling this
     * on every appearance is safe: the service installs its collectors once.
     *
     * Deliberately no pairing check. The only caller is the console screen in its
     * Ready state, and Ready means a pairing was loaded — so checking again would
     * put a keystore read on the startup path, which is the path this app is
     * measured on.
     */
    fun ensureWatching() {
        if (!container.settings.watchInBackground) return
        BridgeWatchService.start(getApplication())
    }

    fun setWatchInBackground(enabled: Boolean) {
        container.settings.watchInBackground = enabled
        _state.value = _state.value.copy(watchInBackground = enabled)
        if (container.pairingStore.load() == null) return
        if (enabled) {
            BridgeWatchService.start(getApplication())
        } else {
            BridgeWatchService.stop(getApplication())
        }
    }

    /**
     * Records whether Android will let this app notify the user.
     *
     * Called from the screen — on the answer to the permission prompt, and again
     * every time the screen comes back to the foreground. The second call is the
     * one that matters later: a user can turn notifications off in Settings at any
     * point, and without re-checking, the app would keep looking healthy while the
     * one thing it exists for stopped working.
     */
    fun setNotificationsBlocked(blocked: Boolean) {
        setDeviceRestrictions(notificationsBlocked = blocked)
    }

    /**
     * Records what this phone reports about running and showing things.
     *
     * Both facts arrive together — they are read from the same place, at the same
     * moments (the answer to the permission prompt, and every return to the
     * foreground) — so they are stored together and the screen renders one list of
     * advice from them (see DeviceAdvice).
     */
    fun setDeviceRestrictions(
        notificationsBlocked: Boolean = _state.value.notificationsBlocked,
        backgroundRestricted: Boolean = _state.value.backgroundRestricted,
    ) {
        val current = _state.value
        if (current.notificationsBlocked == notificationsBlocked &&
            current.backgroundRestricted == backgroundRestricted
        ) {
            return
        }
        _state.value = current.copy(
            notificationsBlocked = notificationsBlocked,
            backgroundRestricted = backgroundRestricted,
        )
    }

    /**
     * Sends a decision and re-reads afterwards.
     *
     * The re-read is the point: the app never assumes the action worked, it looks
     * at what the computer now says. If the action was refused, the user sees the
     * reason the computer gave rather than a hopeful spinner.
     */
    fun decide(decision: Decision, kind: DecisionKind, text: String? = null) {
        submit(decision.id, decision.paneId, kind, text, decision.hostId)
    }

    /**
     * One implementation behind every decision, whoever asked for it.
     *
     * The queue card, the notification action and the escape hatch all funnel
     * through here, so "what happens when you approve something" is written once.
     */
    fun submit(
        decisionId: String,
        paneId: String,
        kind: DecisionKind,
        text: String? = null,
        host: String = "",
    ) {
        _state.value = _state.value.copy(
            busyDecisionId = decisionId.ifBlank { null },
            sessionBusy = _state.value.session != null,
        )
        viewModelScope.launch {
            when (
                val outcome = DecisionExecutor.run(
                    context = getApplication(),
                    decisionId = decisionId,
                    paneId = paneId,
                    kind = kind,
                    text = text,
                    host = host,
                )
            ) {
                is DecisionOutcome.Handled -> _state.value = _state.value.copy(
                    busyDecisionId = null,
                    sessionBusy = false,
                    problem = null,
                    recentWin = dev.perch.console.core.format.Duration.savedClaim(outcome.secondsSaved),
                )
                is DecisionOutcome.Refused -> _state.value = _state.value.copy(
                    busyDecisionId = null,
                    sessionBusy = false,
                    problem = ConsoleState.Problem(outcome.message, outcome.fix),
                )
                is DecisionOutcome.Unreachable -> {
                    // DecisionExecutor has already held the reply; the screen only
                    // has to say so.
                    _state.value = _state.value.copy(
                        busyDecisionId = null,
                        sessionBusy = false,
                        problem = null,
                        queuedReplies = container.outbox.size(),
                        notice = getApplication<Application>().getString(R.string.reply_queued),
                    )
                }
            }
            refresh()
            // The escape hatch shows the session itself, so it has to be re-read
            // too, otherwise the user would be looking at the screen as it was
            // before they acted.
            if (_state.value.session != null) refreshSession()
        }
    }

    fun dismissProblem() {
        _state.value = _state.value.copy(problem = null)
    }

    /**
     * Called when the app first shows something the user can act on.
     *
     * The measurement is from the process starting, not from this call: spec SC-007
     * is about the time a person waits after tapping the icon, which includes
     * everything the process does before anyone can use it. `getStartUptimeMillis`
     * is the only clock that starts early enough to measure that honestly.
     */
    fun noteFirstUsableScreen() {
        if (startupReported) return
        startupReported = true
        val client = container.client() ?: return
        // What the *user* waited, not what the process has been alive for: the
        // watching service keeps this process running from boot, so the process
        // start is usually minutes before the tap. The first real trial measured
        // 9,874 ms that way and blamed the app for the user's own delay — see
        // StartupMeasurement.
        val measurement = StartupMeasurement.measure(
            processStartMillis = Process.getStartUptimeMillis(),
            launchStartMillis = launchStartedAtMillis,
            firstFrameMillis = SystemClock.uptimeMillis(),
        )
        viewModelScope.launch { client.reportColdStart(measurement.millis) }
    }

    /**
     * When this launch began, from the activity that started it.
     *
     * Recorded by [dev.perch.console.MainActivity.onCreate] rather than worked out
     * here, because only the activity knows when the user's tap turned into this
     * screen.
     */
    fun noteLaunchStart(atMillis: Long) {
        launchStartedAtMillis = atMillis
    }

    fun dismissNotice() {
        if (_state.value.notice != null) {
            _state.value = _state.value.copy(notice = null)
        }
    }

    // ------------------------------------------------------------ voice

    /**
     * Starts recording.
     *
     * A failure here is almost always the microphone permission having been
     * refused, so the message says that instead of leaving a button that appears
     * to do nothing.
     */
    fun startVoiceCapture() {
        if (recorder.start() == null) {
            _state.value = _state.value.copy(
                voiceRecording = false,
                problem = ConsoleState.Problem(
                    message = getApplication<Application>().getString(R.string.voice_no_microphone),
                    fix = getApplication<Application>().getString(R.string.voice_no_microphone_fix),
                ),
            )
            return
        }
        _state.value = _state.value.copy(voiceRecording = true, transcript = null)
    }

    /**
     * Stops recording, transcribes it, and puts the words in the reply box.
     *
     * The recording is deleted whatever happens, including on failure: it is a few
     * seconds of the user's voice, and the right amount of it to keep is none. That
     * is also what makes FR-014's privacy claim a property of the code rather than a
     * promise in a policy.
     */
    fun stopVoiceCapture() {
        val file = recorder.stop()
        if (file == null) {
            _state.value = _state.value.copy(
                voiceRecording = false,
                problem = ConsoleState.Problem(
                    message = getApplication<Application>().getString(R.string.voice_too_short),
                    fix = getApplication<Application>().getString(R.string.voice_too_short_fix),
                ),
            )
            return
        }
        val config = container.voiceSettings.load()
        _state.value = _state.value.copy(voiceRecording = false, voiceTranscribing = true)
        viewModelScope.launch {
            val result = if (config == null) {
                Transcription.Failed(
                    reason = getApplication<Application>().getString(R.string.voice_not_configured),
                    fix = getApplication<Application>().getString(R.string.voice_not_configured_fix),
                )
            } else {
                transcriber.transcribe(file, config)
            }
            file.delete()
            _state.value = when (result) {
                is Transcription.Text -> _state.value.copy(
                    voiceTranscribing = false,
                    transcript = result.text,
                    problem = null,
                )
                is Transcription.Failed -> _state.value.copy(
                    voiceTranscribing = false,
                    problem = ConsoleState.Problem(result.reason, result.fix),
                )
            }
        }
    }

    fun discardVoiceCapture() {
        recorder.cancel()
        _state.value = _state.value.copy(voiceRecording = false, voiceTranscribing = false)
    }

    /** Called once a transcript has been put into the reply box. */
    fun clearTranscript() {
        if (_state.value.transcript != null) {
            _state.value = _state.value.copy(transcript = null)
        }
    }

    /** Stores the user's own transcription key, or clears it when the key is blank. */
    fun saveVoiceConfig(apiKey: String, endpoint: String) {
        if (apiKey.isBlank()) {
            container.voiceSettings.clear()
            _state.value = _state.value.copy(
                voiceConfigured = false,
                notice = getApplication<Application>().getString(R.string.voice_setup_removed),
            )
            return
        }
        container.voiceSettings.save(
            VoiceConfig(
                endpoint = endpoint.ifBlank { dev.perch.console.core.voice.VoiceSettings.DEFAULT_ENDPOINT },
                apiKey = apiKey,
                model = dev.perch.console.core.voice.VoiceSettings.DEFAULT_MODEL,
            ),
        )
        _state.value = _state.value.copy(
            voiceConfigured = true,
            notice = getApplication<Application>().getString(R.string.voice_setup_saved),
        )
    }

    // ------------------------------------------------------------ escape hatch

    /**
     * A decision notification was tapped; open the session it is about.
     *
     * If that session is already on screen, do nothing: a tap should not reload
     * the conversation and throw away the reader's position. The host may be
     * absent on an older notification, and "this computer" is the right reading
     * of an absent host (spec FR-016).
     */
    fun openSessionFromNotification(paneId: String, host: String = "") {
        val current = _state.value.session
        if (current != null && current.paneId == paneId &&
            (host.isBlank() || current.hostId == host)
        ) {
            return
        }
        openSession(paneId, host)
    }

    /**
     * Opens a session for reading. Read-only until the user types something.
     *
     * The host comes with it: `w1:p1` exists on every computer, so a session opened
     * by pane id alone could be read from, and typed into, the wrong machine.
     */
    fun openSession(paneId: String, host: String = "", inTerminal: Boolean = false) {
        val client = container.client() ?: return
        _state.value = _state.value.copy(sessionBusy = true)
        viewModelScope.launch {
            val started = System.nanoTime()
            try {
                val view = openSessionPatiently(client, paneId, host)
                // "Why does opening take so long" is a question about a number, so the number goes to the
                // computer's log: anything over a second is worth seeing, and a phone that is merely slow
                // reads differently from one that had to retry a dead connection (the client reports that
                // itself, see `BridgeClient.get`).
                val millis = (System.nanoTime() - started) / 1_000_000
                if (millis > 1000) {
                    reportSessionTiming(paneId, "opened slowly: ${millis}ms")
                }
                val previous = _state.value.session
                // Same session, same conversation: it is about to be re-read, and clearing it would
                // flash the terminal's own text in its place. A *different* session's conversation is
                // dropped here, before it can be drawn under the new session's header.
                val sameSession = previous != null && previous.paneId == view.paneId && previous.hostId == view.hostId
                _state.value = _state.value.copy(
                    session = view,
                    sessionInTerminal = inTerminal,
                    sessionBusy = false,
                    conversation = if (sameSession) _state.value.conversation else null,
                )
                // The conversation comes from the agent's own record, and it is read here rather than
                // inside the poll so the reading face has something on it in the same breath as the
                // screen opening. A failure is not reported: the screen's fallback is the terminal's
                // own text, which is what this face showed before records existed.
                readConversation(client, paneId, host)
                // Opening a session starts the live loop; it is what makes the
                // screen a view of the agent rather than a photograph of it.
                ensureSessionPoll()
            } catch (e: Exception) {
                reportSessionFailure(paneId, e)
                _state.value = _state.value.copy(
                    sessionBusy = false,
                    problem = ConsoleState.Problem(
                        message = (e as? BridgeException)?.message ?: "Could not read that session.",
                        fix = (e as? BridgeException)?.fix
                            ?: "Check that the computer is awake, then try again.",
                    ),
                )
            }
        }
    }

    /** Re-reads the open session. Called after a write, to show what it did. */
    fun refreshSession() {
        _state.value.session?.let { openSession(it.paneId, it.hostId, _state.value.sessionInTerminal) }
    }

    /**
     * Says whether the session screen is in front of the user.
     *
     * Called from the screen's own lifecycle, because only the screen knows: the
     * state holder outlives it (the activity keeps its view model while the app is
     * in the background), so "an open session" and "a session being watched" are
     * two different things and the difference is what the user's battery notices.
     */
    fun noteSessionOnScreen(visible: Boolean) {
        sessionOnScreen = visible
        if (visible) {
            ensureSessionPoll()
        } else {
            sessionPoll?.cancel()
            sessionPoll = null
        }
    }

    /**
     * Says whether the reader is at the end of the session they have open.
     *
     * Used to keep a read from replacing the text somebody is in the middle of reading
     * ([LiveSession.afterRead]). The light, the buttons and a dialog appearing all still
     * arrive while they read — only the words hold still.
     */
    fun noteSessionFollowing(following: Boolean) {
        sessionFollowing = following
    }

    private fun ensureSessionPoll() {
        if (_state.value.terminalOnlySessions) return
        if (!sessionOnScreen) return
        if (sessionPoll?.isActive == true) return
        if (_state.value.session == null) return
        sessionPoll = viewModelScope.launch {
            while (isActive) {
                // The wait depends on what the session is doing: while an agent is
                // writing, this interval *is* the frame rate of the stream.
                val waiting = _state.value.session ?: return@launch
                delay(LiveSession.pollInterval(waiting.agentStatus))
                if (!sessionOnScreen) return@launch
                val current = _state.value
                // A different pane is a different read, so read the pane the screen
                // is on now rather than the one this loop started with.
                val view = current.session ?: return@launch
                if (current.sessionBusy) continue
                val client = container.client() ?: return@launch
                val next = try {
                    // The depth the screen is already holding, not a fixed window: a poll that read
                    // less than what is on screen would take history away from the reader a second
                    // after they opened it (see LiveSession.pollDepth).
                    client.session(
                        view.paneId,
                        lines = LiveSession.pollDepth(view.text),
                        host = view.hostId,
                    )
                } catch (_: Exception) {
                    continue
                }
                val merged = LiveSession.afterRead(view, next, sessionFollowing)
                if (LiveSession.differs(view, merged)) {
                    _state.value = _state.value.copy(session = merged)
                }
                // The conversation, on the same tick: it is what the reading face draws, and it is
                // the thing that has to arrive as the agent writes. The record reader behind it tails
                // the file, so this is a few milliseconds on the computer after the first read.
                readConversation(client, view.paneId, view.hostId)
            }
        }
    }

    /**
     * Reads the open session's conversation and keeps it only if it changed.
     *
     * Both halves are deliberate. It is *read* on every tick rather than only when an agent is
     * working, because the agent is not the only thing that can add to a conversation — the person at
     * the keyboard can type a sentence too, and a phone that only looked while an agent was busy would
     * miss exactly the turns that explain what happened next. It is *kept* only when it differs, so a
     * quiet session does not recompose the reading area once a second.
     */
    private suspend fun readConversation(client: BridgeClient, paneId: String, host: String) {
        if (_state.value.terminalOnlySessions) return
        val next = try {
            client.conversation(paneId, host)
        } catch (_: Exception) {
            // Silence is not news: the connection line already reports being offline, and an error
            // here would replace what the user is reading with something they cannot act on.
            return
        }
        // The pane can change while this read is in flight — the user switches session, and this
        // answer arrives afterwards. Drawing one session's conversation under another session's
        // header is the worst thing this screen could do, so it is checked at the last moment.
        val current = _state.value.session ?: return
        if (current.paneId != paneId || current.hostId != host) return
        val shown = _state.value.conversation
        if (shown != null && !LiveSession.conversationDiffers(shown, next)) return
        _state.value = _state.value.copy(conversation = next)
    }

    fun closeSession() {
        sessionPoll?.cancel()
        sessionPoll = null
        _state.value = _state.value.copy(
            session = null,
            sessionUsage = null,
            sessionUsageBusy = false,
            conversation = null,
        )
    }

    /**
     * A fresh connection to the open session's real terminal.
     *
     * A factory rather than a connection, because the screen owns the lifetime: it opens one when the
    * terminal is shown and drops it when the terminal is closed, and a fresh attach is what makes
     * reconnecting safe (the terminal repaints the current screen; nothing is replayed).
     *
     * No size is passed, and that is deliberate: the pane's terminal belongs to the program running in
     * it and to the person at the computer, and a phone that reshaped it would be moving their screen
     * (see [BridgeClient.terminalSocket] and the bridge's `PaneGeometry`). The shape travels the other
     * way — from the computer to this screen.
    */
    fun terminalConnection(): TerminalConnection? {
        val view = _state.value.session ?: return null
        val client = container.client() ?: return null
        return TerminalConnection(
            client = client,
            paneId = view.paneId,
            hostId = view.hostId,
        )
    }

    private suspend fun openSessionPatiently(
        client: dev.perch.console.core.herdrclient.BridgeClient,
        paneId: String,
        host: String,
    ): SessionView {
        val pauses = listOf(0L, 2_000L, 6_000L)
        var last: Exception? = null
        for ((attempt, pause) in pauses.withIndex()) {
            if (pause > 0) {
                reportSessionTiming(paneId, "still opening — attempt ${attempt + 1} after ${pause}ms")
                kotlinx.coroutines.delay(pause)
            }
            try {
                return client.session(paneId, lines = LiveSession.READ_LINES, host = host)
            } catch (e: Exception) {
                last = e
                reportSessionFailure(paneId, e, attempt + 1)
            }
        }
        throw last ?: IllegalStateException("could not open that session")
    }

    /** One line about how long opening a session took, when that was long enough to notice. */
    private fun reportSessionTiming(paneId: String, detail: String) {
        val client = container.client() ?: return
        viewModelScope.launch {
            client.reportTerminal(paneId = paneId, event = "app", detail = detail)
        }
    }

    /**
     * Says, on the computer, why opening a session failed.
     *
     * The user's banner has one sentence for every cause — a dropped socket, a pane herdr refuses, a
     * bridge that answered something unexpected — which is right for a phone (they cannot act on the
     * difference) and useless for whoever is looking at the host. This is the diagnosis.
     */
    private fun reportSessionFailure(paneId: String, failure: Exception, attempt: Int = 1) {
        val client = container.client() ?: return
        val what = failure::class.simpleName ?: "unknown failure"
        val detail = listOfNotNull(what, failure.message?.takeIf { it.isNotBlank() }).joinToString(": ")
        viewModelScope.launch {
            client.reportTerminal(
                paneId = paneId,
                event = "app",
                detail = "open failed (attempt $attempt) — $detail",
            )
            // And then: is the link alive *now*? One small request settles whether the failure was the
            // phone's link being asleep (health is slow or fails too) or something about that one request
            // (health answers in milliseconds). Without this, the next round is a guess again.
            val started = System.nanoTime()
            val outcome = runCatching { client.health() }
            val millis = (System.nanoTime() - started) / 1_000_000
            val health = if (outcome.isSuccess) "ok in ${millis}ms" else {
                "failed after ${millis}ms (${outcome.exceptionOrNull()?.let { it::class.simpleName }})"
            }
            client.reportTerminal(
                paneId = paneId,
                event = "app",
                detail = "health right after that: $health",
            )
        }
    }

    /**
     * Sends one line about the terminal screen to the computer's log.
     *
     * Fire and forget, and deliberately not routed through state: this exists so that a failure inside
     * the WebView leaves a trace on the host, and a report about the screen must never be able to break
     * the screen. A failure to report is itself not worth reporting.
     */
    /**
     * Sends a picture of the terminal screen to the computer, because the user pressed the button.
     *
     * The answer goes into the same log as everything else about the terminal — the path it was saved to,
     * or why it could not be sent. A debug picture that silently fails would be the same joke as the
     * reports that were never wired at all.
     */
    fun sendScreenshot(png: ByteArray) {
        val view = _state.value.session ?: return
        val client = container.client() ?: return
        val encoded = android.util.Base64.encodeToString(png, android.util.Base64.NO_WRAP)
        viewModelScope.launch {
            val saved = client.reportScreenshot(paneId = view.paneId, pngBase64 = encoded)
            client.reportTerminal(
                paneId = view.paneId,
                event = "app",
                detail = if (saved != null) "screenshot saved: $saved" else "could not send the screenshot",
            )
        }
    }

    fun reportTerminal(event: String, detail: String) {
        val view = _state.value.session ?: return
        val client = container.client() ?: return
        viewModelScope.launch {
            client.reportTerminal(view.paneId, event, detail)
        }
    }

    /**
     * A terminal on the computer: the shell, not a session's screen.
     *
     * Its own state (`shellOpen`) rather than a flag inside the session, because a shell is not about a
     * pane: it can be opened from the console with or without a session in front of it, and closing the
     * session must not close it.
     */
    fun openShell() {
        _state.value = _state.value.copy(shellOpen = true, shellFresh = false)
    }

    fun closeShell() {
        // Closing the screen does not end the shell: the process stays on the computer, and the next open
        // reattaches to it with the recent output replayed. Walking away is what a phone does.
        _state.value = _state.value.copy(shellOpen = false, shellFresh = false)
    }

    /**
     * A connection to the shell, or null when the phone is not paired.
     *
     * `fresh` is "new terminal": the bridge ends the running shell and starts another. Every reconnect — a
     * rotation, a dropped network, a forgotten phone — uses `fresh = false`, because a phone walking away
     * must not take the running build with it; that is the property the whole design is for.
     */
    fun shellConnection(fresh: Boolean): ShellConnection? {
        val client = container.client() ?: return null
        if (fresh) _state.value = _state.value.copy(shellFresh = true)
        return ShellConnection(client = client, fresh = fresh)
    }

    /**
     * Sends one line about the shell screen to the computer's log.
     *
     * The pane id is the word "shell": this report is about a terminal that belongs to no pane, and a reader
     * of the host log should not have to guess why there is no pane id.
     */
    fun reportShell(event: String, detail: String) {
        val client = container.client() ?: return
        viewModelScope.launch {
            client.reportTerminal(paneId = SHELL_PANE_ID, event = event, detail = detail)
        }
    }

    // ------------------------------------------------- naming and ending things
    //
    // The four actions behind the two new endpoints (2026-10-15). They share one shape: send, then say what
    // happened in the product's words, then re-read the computer so the list on screen is the list that is
    // true. The last part is not politeness: a rename that only changed a label in this app's memory would
    // look right until the next refresh and then revert, which is the kind of bug that makes somebody stop
    // trusting a screen.

    /** Names the open session, or clears its name when [label] is blank. */
    fun renameSession(paneId: String, host: String, label: String) {
        val client = container.client() ?: return
        viewModelScope.launch {
            val message = runCatching { client.rename(ManageTarget.Session, paneId, label.trim(), host) }
            finishManage(
                message = message,
                named = label.trim().ifBlank { "" },
                closedSession = null,
            )
        }
    }

    /** Names a project. */
    fun renameProject(workspaceId: String, host: String, label: String) {
        val client = container.client() ?: return
        viewModelScope.launch {
            val message = runCatching { client.rename(ManageTarget.Project, workspaceId, label.trim(), host) }
            finishManage(message = message, named = label.trim(), closedSession = null)
        }
    }

    /**
     * Ends a session: the program in the pane stops and the pane goes away.
     *
     * If the session in front of the user is the one that went, the screen closes with it — a session screen
     * for a pane that no longer exists would keep polling a ghost.
     */
    fun closeSession(paneId: String, host: String) {
        val client = container.client() ?: return
        viewModelScope.launch {
            val message = runCatching { client.close(ManageTarget.Session, paneId, host) }
            finishManage(message = message, named = null, closedSession = paneId)
        }
    }

    /**
     * Closes a project and every session in it. [confirmed] is the user's own word, from a dialog.
     */
    fun closeProject(workspaceId: String, host: String, confirmed: Boolean) {
        val client = container.client() ?: return
        viewModelScope.launch {
            val message = runCatching { client.close(ManageTarget.Project, workspaceId, host, confirmed) }
            finishManage(message = message, named = null, closedSession = null)
        }
    }

    fun loadHarnesses(host: String = "") {
        val client = container.client() ?: return
        if (_state.value.harnessesBusy) return
        _state.value = _state.value.copy(harnessesBusy = true, harnesses = emptyList())
        viewModelScope.launch {
            runCatching { client.harnesses(host) }
                .onSuccess { response ->
                    _state.value = _state.value.copy(
                        harnesses = response.harnesses,
                        harnessesBusy = false,
                        homeFolder = response.defaultCwd,
                    )
                }
                .onFailure { failure ->
                    _state.value = _state.value.copy(
                        harnessesBusy = false,
                        problem = ConsoleState.Problem(
                            message = failure.message ?: getApplication<Application>()
                                .getString(R.string.manage_failed),
                            fix = getApplication<Application>().getString(R.string.manage_failed_fix),
                        ),
                    )
                }
        }
    }

    /**
     * Lists one folder on the computer for the working-folder picker.
     *
     * Suspending rather than pushed through [ConsoleState] on purpose: the picker is the only
     * caller, it is open for seconds, and a listing is stale the moment the person taps something
     * else. The screen holds the result while the dialog is up; the computer stays the authority.
     */
    suspend fun listFolders(path: String, host: String = ""): Result<FoldersResponse> {
        val client = container.client()
            ?: return Result.failure(
                IllegalStateException(getApplication<Application>().getString(R.string.manage_failed)),
            )
        // Not `runCatching`: the picker is closed by leaving the dialog, which cancels its effect,
        // and swallowing CancellationException would keep the request's failure path running after
        // the screen that asked for it is gone.
        return try {
            Result.success(client.folders(path, host))
        } catch (cancelled: kotlinx.coroutines.CancellationException) {
            throw cancelled
        } catch (failure: Exception) {
            Result.failure(failure)
        }
    }

    /** Clears the completion of a previous creation when its dialog opens again. */
    fun resetCreate() {
        if (_state.value.createDone) {
            _state.value = _state.value.copy(createDone = false)
        }
    }

    fun createProject(
        label: String,
        cwd: String,
        kind: String,
        name: String,
        args: List<String>,
        host: String = "",
    ) {
        if (_state.value.createBusy) return
        _state.value = _state.value.copy(createBusy = true, createDone = false)
        val client = container.client()
        if (client == null) {
            _state.value = _state.value.copy(createBusy = false)
            return
        }
        viewModelScope.launch {
            val result = runCatching { client.createProject(label, cwd, kind, name, args, host) }
            if (result.isSuccess) {
                // Only a folder a project was actually created in becomes "recent". A folder the
                // person merely looked at is not a place they work, and the picker's first row is
                // supposed to be where they will want to start next time.
                container.settings.rememberFolder(cwd)
                _state.value = _state.value.copy(recentFolders = container.settings.recentFolders)
            }
            finishCreate(result, label)
        }
    }

    fun startSession(
        workspaceId: String,
        kind: String,
        name: String,
        args: List<String>,
        host: String = "",
    ) {
        if (_state.value.createBusy) return
        _state.value = _state.value.copy(createBusy = true, createDone = false)
        val client = container.client()
        if (client == null) {
            _state.value = _state.value.copy(createBusy = false)
            return
        }
        viewModelScope.launch {
            val result = runCatching { client.createSession(workspaceId, kind, name, args, host) }
            finishCreate(result, name.ifBlank { kind })
        }
    }

    private suspend fun finishCreate(
        result: Result<dev.perch.console.core.herdrclient.CreateResult>,
        label: String,
    ) {
        result.onSuccess { created ->
            val event = getApplication<Application>().getString(R.string.manage_created, label)
            _state.value = _state.value.copy(
                createBusy = false,
                createDone = true,
                problem = null,
                notice = if (created.equivalent.isBlank()) event else "$event · ${created.equivalent}",
            )
        }.onFailure { failure ->
            _state.value = _state.value.copy(
                createBusy = false,
                createDone = false,
                problem = ConsoleState.Problem(
                    message = failure.message ?: getApplication<Application>()
                        .getString(R.string.manage_failed),
                    fix = getApplication<Application>().getString(R.string.manage_create_failed_fix),
                ),
            )
        }
        if (result.isSuccess) refresh()
    }

    /**
     * What every one of those four does afterwards.
     *
     * Written once because the four differ only in what they send: success says what happened *and* re-reads
     * the computer, failure becomes a problem with a fix, and a closed session takes its own screen with it.
     */
    private suspend fun finishManage(
        message: Result<ManageResult>,
        named: String?,
        closedSession: String?,
    ) {
        message.onSuccess { result ->
            val event = if (named != null) {
                if (named.isEmpty()) {
                    getApplication<Application>().getString(R.string.manage_name_cleared)
                } else {
                    getApplication<Application>().getString(R.string.manage_renamed, named)
                }
            } else {
                getApplication<Application>().getString(R.string.manage_closed)
            }
            _state.value = _state.value.copy(
                // The herdr command travels with the sentence, the way the decision cards do it: the user can
                // read exactly what was done on their own computer, and can type it again if they want it back.
                notice = if (result.equivalent.isBlank()) event else "$event · ${result.equivalent}",
                problem = null,
            )
        }
        message.onFailure { failure ->
            _state.value = _state.value.copy(
                problem = ConsoleState.Problem(
                    message = failure.message ?: getApplication<Application>()
                        .getString(R.string.manage_failed),
                    fix = getApplication<Application>().getString(R.string.manage_failed_fix),
                ),
            )
        }
        if (closedSession != null && message.isSuccess && _state.value.session?.paneId == closedSession) {
            closeSession()
            return
        }
        // Re-read the computer rather than patching the label here: the list on the phone should be the list
        // the computer has, and this is the one write where a local guess would be invisible until later.
        refresh()
    }

   /**
    * Asks the computer what the harness itself knows: model, spending, goal.
     *
     * On demand, when the panel is opened, and kept until the session is closed. The answer
     * costs the computer a read of the harness's own record file (tens of megabytes on a long
     * session), which is why this is a button and not a poll. A failure is silent on purpose:
     * the panel says "nothing to show", and a banner about a panel nobody needs would be noise
     * in front of the answer they are reading.
     */
    fun loadSessionUsage() {
        val view = _state.value.session ?: return
        if (_state.value.sessionUsageBusy) return
        val client = container.client() ?: return
        _state.value = _state.value.copy(sessionUsageBusy = true)
        viewModelScope.launch {
            val facts = try {
                client.usage(view.paneId, host = view.hostId)
            } catch (_: Exception) {
                dev.perch.console.core.herdrclient.UsageFacts()
            }
            _state.value = _state.value.copy(sessionUsage = facts, sessionUsageBusy = false)
        }
    }

    /** Approves the request the open session is waiting on, if it is waiting. */
    fun approveSession() {
        val view = _state.value.session ?: return
        if (view.decisionId.isBlank()) return
        submit(view.decisionId, view.paneId, DecisionKind.APPROVE, host = view.hostId)
    }

    /** Declines the request the open session is waiting on. */
    fun rejectSession() {
        val view = _state.value.session ?: return
        if (view.decisionId.isBlank()) return
        submit(view.decisionId, view.paneId, DecisionKind.REJECT, host = view.hostId)
    }

    /**
     * Delivers what the user typed into the open session.
     *
     * Uses the same endpoint and the same mapper as a decision: the escape hatch
     * does not get its own write path, because a second path is a second place for
     * the four-write rule to be forgotten.
     */
    fun sendToSession(text: String) {
        val view = _state.value.session ?: return
        if (text.isBlank()) return
        // One delivery at a time. The button is disabled while an attempt is in
        // flight, but "disabled" is a promise the UI makes; this is the one the code
        // keeps. Two sends of the same sentence would type it into the session twice,
        // and the user would have to work out why their message appeared twice.
        if (_state.value.sessionBusy) return
        _state.value = _state.value.copy(sessionBusy = true)
        viewModelScope.launch {
            val client = container.client()
            val outcome = DecisionExecutor.run(
                context = getApplication(),
                decisionId = "",
                paneId = view.paneId,
                kind = DecisionKind.PROMPT,
                text = text,
                host = view.hostId,
            )
            when (outcome) {
                is DecisionOutcome.Handled -> {
                    val sent = getApplication<Application>().getString(
                        when {
                            view.hasNoAgent -> R.string.session_sent_shell
                            view.agentStatus == AgentStates.BLOCKED -> R.string.session_sent_blocked
                            else -> R.string.session_sent_agent
                        },
                    )
                    _state.value = _state.value.copy(sessionBusy = false, notice = sent)
                    // Show the effect rather than asserting it: the screen is the
                    // evidence, and the user is standing in front of it.
                    client?.let { openSession(view.paneId, view.hostId, _state.value.sessionInTerminal) }
                }
                is DecisionOutcome.Refused -> _state.value = _state.value.copy(
                    sessionBusy = false,
                    problem = ConsoleState.Problem(outcome.message, outcome.fix),
                )
                is DecisionOutcome.Unreachable -> _state.value = _state.value.copy(
                    sessionBusy = false,
                    problem = ConsoleState.Problem(
                        message = getApplication<Application>().getString(R.string.failure_unreachable),
                        fix = getApplication<Application>().getString(R.string.failure_unreachable_fix),
                    ),
                )
            }
        }
    }

    /** Called by the UI once the confirmation has been on screen long enough. */
    fun clearRecentWin() {
        if (_state.value.recentWin != null) {
            _state.value = _state.value.copy(recentWin = null)
        }
    }
}
