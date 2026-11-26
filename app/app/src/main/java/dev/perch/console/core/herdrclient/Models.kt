package dev.perch.console.core.herdrclient

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/*
 * Wire models for the bridge protocol (contracts/app-bridge.md).
 *
 * Two deliberate choices run through this file:
 *
 *  1. Everything the computer owns is *mirrored*, never re-derived. `agentStatus`
 *     is a String rather than an enum on purpose: herdr may introduce a state
 *     this build has never heard of, and a decoding failure would turn "we do
 *     not know" into "the app is broken". The UI maps the string and shows
 *     anything unrecognised as unknown, which is what the spec's edge case
 *     demands.
 *  2. Every type is defaulted, so a field added or removed by a future bridge
 *     build degrades instead of crashing.
 */

// ---------------------------------------------------------------- mirror

@Serializable
data class Mirror(
    val hosts: List<Host> = emptyList(),
    @SerialName("herdr_version") val herdrVersion: String = "",
    val protocol: Int = 0,
    @SerialName("taken_at") val takenAt: String = "",
    val workspaces: List<Workspace> = emptyList(),
    val tabs: List<Tab> = emptyList(),
    val panes: List<Pane> = emptyList(),
    val agents: List<Agent> = emptyList(),
) {
    fun panesOf(hostId: String, workspaceId: String): List<Pane> =
        panes.filter { sameHost(it.hostId, hostId) && it.workspaceId == workspaceId }

    fun workspace(hostId: String, workspaceId: String): Workspace? =
        workspaces.firstOrNull { sameHost(it.hostId, hostId) && it.workspaceId == workspaceId }

    /** Every computer that has something to show, this one first. */
    fun hostIdsWithContent(): List<String> {
        val named = hosts.map { normalizeHost(it.hostId) }
        val fromContent = workspaces.map { normalizeHost(it.hostId) }
        return (named + fromContent).distinct()
    }

    /** The name to show for a computer, falling back to the id it came with. */
    fun hostLabel(hostId: String): String {
        val id = normalizeHost(hostId)
        return hosts.firstOrNull { sameHost(it.hostId, id) }?.label?.takeIf { it.isNotBlank() } ?: id
    }

    /**
     * True when naming the computer on every card earns the space.
     *
     * With one machine the name is noise — it is the user's own computer, and they
     * know it. The moment a second machine answers, "which computer is this on?"
     * becomes a real question, and every list answers it.
     */
    val showHostNames: Boolean
        get() = hosts.count { sameHost(it.hostId, Host.LOCAL) || it.status == WireValues.HostStatus.REACHABLE } > 1

    companion object {
        /*
         * A record with no host belongs to this computer.
         *
         * Two reasons this has to be handled rather than assumed away: a mirror
         * stored by an older bridge has no host fields at all, and the local
         * machine always did mean "local" before there was more than one. Getting
         * it wrong is not subtle — the first version of the grouped project list
         * dropped every workspace whose host was empty, which is how this was
         * found: the rendered screen simply had no projects on it.
         */
        fun normalizeHost(hostId: String): String = hostId.ifBlank { Host.LOCAL }

        fun sameHost(a: String, b: String): Boolean = normalizeHost(a) == normalizeHost(b)
    }
}

@Serializable
data class Host(
    @SerialName("host_id") val hostId: String = "",
    val label: String = "",
    val kind: String = "",
    val status: String = "",
    /** Why an unreachable computer is unreachable, and what to do about it. */
    val reason: String = "",
    val fix: String = "",
    val enabled: Boolean = true,
) {
    companion object {
        /** The machine the bridge runs on; the only host id that is not a label. */
        const val LOCAL = "local"
    }
}

@Serializable
data class Workspace(
    @SerialName("workspace_id") val workspaceId: String = "",
    val number: Int = 0,
    val label: String = "",
    val focused: Boolean = false,
    @SerialName("pane_count") val paneCount: Int = 0,
    @SerialName("tab_count") val tabCount: Int = 0,
    @SerialName("active_tab_id") val activeTabId: String = "",
    @SerialName("agent_status") val agentStatus: String = WireValues.AgentStatus.UNKNOWN,
    @SerialName("host_id") val hostId: String = "",
)

@Serializable
data class Tab(
    @SerialName("tab_id") val tabId: String = "",
    @SerialName("workspace_id") val workspaceId: String = "",
    val number: Int = 0,
    val label: String = "",
    val focused: Boolean = false,
    @SerialName("pane_count") val paneCount: Int = 0,
    @SerialName("agent_status") val agentStatus: String = WireValues.AgentStatus.UNKNOWN,
    @SerialName("host_id") val hostId: String = "",
)

@Serializable
data class Pane(
    @SerialName("pane_id") val paneId: String = "",
    @SerialName("workspace_id") val workspaceId: String = "",
    @SerialName("tab_id") val tabId: String = "",
    val focused: Boolean = false,
    val agent: String? = null,
    @SerialName("display_agent") val displayAgent: String? = null,
    @SerialName("agent_status") val agentStatus: String = WireValues.AgentStatus.UNKNOWN,
    val cwd: String? = null,
    val title: String? = null,
    @SerialName("terminal_title") val terminalTitle: String? = null,
    @SerialName("host_id") val hostId: String = "",
)

@Serializable
data class Agent(
    val agent: String? = null,
    @SerialName("display_agent") val displayAgent: String? = null,
    val name: String? = null,
    @SerialName("agent_status") val agentStatus: String = WireValues.AgentStatus.UNKNOWN,
    @SerialName("pane_id") val paneId: String = "",
    @SerialName("workspace_id") val workspaceId: String = "",
    val title: String? = null,
)

// ---------------------------------------------------------------- queue

@Serializable
data class Decision(
    val id: String = "",
    @SerialName("host_id") val hostId: String = "",
    @SerialName("pane_id") val paneId: String = "",
    @SerialName("workspace_id") val workspaceId: String = "",
    @SerialName("workspace_label") val workspaceLabel: String = "",
    @SerialName("agent_kind") val agentKind: String = "",
    @SerialName("display_agent") val displayAgent: String = "",
    val title: String = "",
    val rule: String = "",
    @SerialName("prompt_text") val promptText: String = "",
    @SerialName("suggested_kind") val suggestedKind: String = "approve",
    val approvable: Boolean = false,
    val rejectable: Boolean = false,
    /**
     * Whether a typed reply can actually be delivered to this dialog.
     *
     * Separate from approvable/rejectable because it rests on different evidence:
     * answering means writing text into the dialog, and most dialogs are menus.
     * Offering "Answer" on a menu would type a sentence into a menu.
     */
    val answerable: Boolean = false,
    @SerialName("unmappable_reason") val unmappableReason: String? = null,
    @SerialName("created_at") val createdAt: String = "",
    @SerialName("state") val state: String = WireValues.DecisionState.PENDING,
    @SerialName("state_reason") val stateReason: String? = null,
) {
    val agentLabel: String get() = displayAgent.ifBlank { agentKind.ifBlank { "agent" } }
    val projectLabel: String get() = workspaceLabel.ifBlank { "a project" }

    /**
     * The actions that may be offered for this request.
     *
     * A single source for both the in-app card and the notification, so the two
     * cannot disagree about what is possible. Empty means nothing can be pressed
     * from the phone, and the UI must say so instead of showing a dead button.
     */
    val availableActions: List<DecisionKind>
        get() = buildList {
            if (approvable) add(DecisionKind.APPROVE)
            if (rejectable) add(DecisionKind.REJECT)
        }

    /**
     * True when nothing at all can be done from here.
     *
     * All three capabilities count: a dialog whose keys we cannot determine may
     * still be answerable by typing, and saying "go to your computer" while a
     * perfectly good reply box is available would be wrong in the direction that
     * costs the user a trip.
     */
    val needsComputer: Boolean get() = !approvable && !rejectable && !answerable
}

/** The four write classes. There is no fifth (constitution IV). */
enum class DecisionKind(val wire: String) {
    APPROVE("approve"),
    REJECT("reject"),
    ANSWER("answer"),
    PROMPT("prompt"),
}

@Serializable
data class DecisionRequest(
    @SerialName("decision_id") val decisionId: String,
    val kind: String,
    val target: String,
    val text: String? = null,
    /**
     * Which computer the target lives on. Empty means the one Perch runs on.
     *
     * It travels with the target because a pane id is only unique within one
     * machine: `w1:p1` exists on every computer, and a tap that lost this field
     * would press a key on whichever machine the bridge happened to pick.
     */
    val host: String = "",
)

@Serializable
data class DecideResponse(
    val decision: Decision = Decision(),
    @SerialName("equivalent_command") val equivalentCommand: List<String> = emptyList(),
    @SerialName("seconds_saved") val secondsSaved: Int = 0,
)

/**
 * One press on the escape hatch's key bar.
 *
 * A list rather than a single key because that is what the far end offers, but
 * the app sends one at a time: a key bar is pressed, not composed, and a batch
 * would make "which press did that?" unanswerable if something went wrong.
 */
@Serializable
data class KeysRequest(
    @SerialName("pane_id") val paneId: String,
    val keys: List<String>,
    /** See [DecisionRequest.host]. */
    val host: String = "",
)

/** What the computer did, in the one form that can be checked by hand. */
@Serializable
data class KeysResponse(
    @SerialName("equivalent_command") val equivalentCommand: List<String> = emptyList(),
)

/**
 * What a rename or a close is about.
 *
 * "Session" is one pane with an agent in it; "project" is a workspace. The two words are the product's, and
 * they are the only place the wire strings live, so a call site cannot invent a third.
 */
enum class ManageTarget(val wire: String) {
    Session("session"),
    Project("project"),
}

/** A rename request: what to name, and what to call it. */
@Serializable
data class RenameRequest(
    val target: String,
    val id: String,
    val label: String,
    val host: String = "",
)

/** A close request. [confirmed] is required by the bridge for a project, and ignored for a session. */
@Serializable
data class CloseRequest(
    val target: String,
    val id: String,
    val host: String = "",
    val confirmed: Boolean = false,
)

/**
 * What the computer did, as one herdr command.
 *
 * The same promise the decision path makes: every change this product makes is one command somebody could
 * have typed at their own computer, and here is the command. [label] is what it is called now (empty after a
 * cleared session name).
 */
@Serializable
data class ManageResult(
    @SerialName("equivalent") val equivalent: String = "",
    @SerialName("target") val target: String = "",
    @SerialName("id") val id: String = "",
    @SerialName("label") val label: String = "",
)

@Serializable
data class Harness(
    val kind: String = "",
    val executable: String = "",
    val path: String = "",
    val ready: Boolean = true,
    @SerialName("needs_setup") val needsSetup: Boolean = false,
    val reason: String = "",
    val fix: String = "",
)

@Serializable
data class HarnessesResponse(
    val harnesses: List<Harness> = emptyList(),
    @SerialName("default_cwd") val defaultCwd: String = "",
)

/**
 * One folder the working-folder picker can open.
 *
 * The bridge sends the path rather than letting the phone join [name] onto the parent: path joining
 * is the kind of thing that is right on Linux and subtly wrong on Windows, and the computer already
 * knows which separators its own folders use.
 */
@Serializable
data class Folder(
    val name: String = "",
    val path: String = "",
)

/** One directory, as the bridge's folder picker sees it. */
@Serializable
data class FoldersResponse(
    val path: String = "",
    val parent: String = "",
    val home: String = "",
    val entries: List<Folder> = emptyList(),
    val truncated: Boolean = false,
)

@Serializable
data class CreateProjectRequest(
    val label: String,
    val cwd: String = "",
    val kind: String,
    val name: String = "",
    val args: List<String> = emptyList(),
    val host: String = "",
)

@Serializable
data class CreateSessionRequest(
    @SerialName("workspace_id") val workspaceId: String,
    val kind: String,
    val name: String = "",
    val args: List<String> = emptyList(),
    val host: String = "",
)

@Serializable
data class CreateResult(
    @SerialName("workspace_id") val workspaceId: String = "",
    @SerialName("tab_id") val tabId: String = "",
    @SerialName("pane_id") val paneId: String = "",
    val equivalent: String = "",
)

/**
 * A name the phone will accept, checked before anything is sent.
 *
 * The bridge checks the same rules (`herdr.CleanLabel`), and this is not duplication for its own sake: a name
 * that is too long or has a newline in it should be refused *in the dialog*, with the character count in
 * front of the person typing, rather than after a round trip that leaves a red banner over their session.
 * The bridge remains the authority; this is the courtesy.
 */
object ManageLabels {
    const val MAX = 80

    /** The problem with a name, or null when it is fine. */
    fun problem(label: String): String? {
        val clean = label.trim()
        return when {
            clean.isEmpty() -> "A name cannot be empty."
            clean.length > MAX -> "A name can be at most $MAX characters."
            clean.any { it.isISOControl() } -> "A name cannot contain line breaks or tabs."
            else -> null
        }
    }
}

@Serializable
data class PushRegistration(val deviceToken: String)

/**
 * A picture of the phone's screen, on its way to the user's own computer.
 *
 * Base64 in a JSON body rather than multipart, for one reason: the wire the app already speaks is JSON,
 * and a second content type would be a second thing to get wrong for the sake of a debug picture.
 */
@Serializable
data class ScreenshotReport(
    @SerialName("pane_id") val paneId: String = "",
    @SerialName("png_base64") val pngBase64: String = "",
)

/** What the computer says after saving it. */
@Serializable
data class ScreenshotSaved(val saved: String = "", val bytes: Int = 0)

/**
 * How long the phone took to show a notification.
 *
 * A duration, not a timestamp, and that is the point: the bridge and the phone
 * have no reason to agree on a clock, and a measurement that needed them to would
 * be both wrong across timezones and impossible to trust.
 */
@Serializable
data class NotifiedReport(
    @SerialName("decision_id") val decisionId: String,
    @SerialName("shown_after_ms") val shownAfterMs: Long,
)

/** How long the app took to be usable, as only the phone can measure it. */
@Serializable
data class StartupReport(@SerialName("startup_ms") val startupMillis: Long)

/**
 * One line about the terminal screen, for the host's log.
 *
 * The terminal's failures happen inside a WebView where nothing on the computer can see them; this is how
 * they become readable afterwards. [event] is one of [TerminalEvents]; [detail] is whatever makes the line
 * useful — a reason, a size, a JavaScript error.
 */
@Serializable
data class TerminalReport(
    @SerialName("pane_id") val paneId: String,
    val event: String,
    val detail: String,
)

/**
 * What the phone calls the terminal in its reports, because a shell has no pane id.
 *
 * The bridge's report endpoint is keyed by pane (`/v1/feedback/terminal`), and a shell on the computer
 * belongs to no pane at all. "shell" is also the word the bridge's own log uses for this stream, so one
 * thing has one name on both sides — and a reader of the host log does not have to work out why there is
 * no pane id.
 */
const val SHELL_PANE_ID = "shell"

/**
 * The vocabulary of terminal reports, as the phone sends them.
 *
 * Not a contract either side enforces — the host writes these into a log line rather than branching on
 * them — but named here so that a typo is a compile error on the side that matters.
 */
object TerminalEvents {
    const val OPENED = "opened"
    const val SHAPE = "shape"
    /**
     * The page's own grid, for a terminal the phone owns.
     *
     * The opposite direction from [SHAPE]: a pane herdr owns tells the phone how big *it* is, while a
     * shell of the phone's own has no shape until the phone measures its glass and says so — which the app
     * then sends to the computer as a resize, so the shell (and any full-screen program in it) reflows for
     * the phone.
     */
    const val SIZE = "size"
    const val PAGE = "page"
    const val SHAPED = "shaped"
    const val BYTES = "bytes"
    /**
     * A pinch or a double tap: what the reader did to the *drawing*, not to the computer.
     *
     * Sent by the page (see `assets/terminal/index.html`), never by this app: the zoom is the phone's
     * opinion of the pane, and nothing about it travels to the host. It is in this vocabulary because a
     * zoom that went wrong would otherwise be invisible from the computer.
     */
    const val ZOOM = "zoom"
    /**
     * What the platform thinks of the WebView: its size, whether it is attached, whether it is visible.
     *
     * A terminal that is black while its page reports that it drew bytes is either a page problem, a box
     * problem, or a *view* problem — and this is the third of those, answered by the side that owns the
     * view. Added on 2026-10-15, after a day of blind debugging, together with the wiring fix that made
     * any of these lines arrive at all.
     */
    const val VIEW = "view"
    /**
     * What the page's own DOM holds: rows, their first text, colour and height.
     *
     * The witness that separates "the page drew nothing" from "the page drew and the WebView shows nothing" —
     * two different bugs with two different owners.
     */
    const val DOM = "dom"
    const val ENDED = "ended"
    const val ERROR = "error"
}

// ---------------------------------------------------------------- health

@Serializable
data class HealthItem(
    val id: String = "",
    val title: String = "",
    val status: String = "unknown",
    val detail: String = "",
    val fix: String = "",
)

@Serializable
data class Health(
    @SerialName("checked_at") val checkedAt: String = "",
    @SerialName("herdr_version") val herdrVersion: String = "",
    val protocol: Int = 0,
    val items: List<HealthItem> = emptyList(),
    val overall: String = "unknown",
) {
    fun item(id: String): HealthItem? = items.firstOrNull { it.id == id }
}

// ---------------------------------------------------------------- envelopes

@Serializable
data class SnapshotResponse(
    val mirror: Mirror = Mirror(),
    val decisions: List<Decision> = emptyList(),
)

@Serializable
data class DecisionsResponse(val decisions: List<Decision> = emptyList())

@Serializable
data class HealthResponse(
    @SerialName("bridge_version") val bridgeVersion: String = "",
    /**
     * The wire contract the computer speaks; 0 when it did not say (an older
     * bridge). See [ApiCompatibility] for what is done with it.
     */
    @SerialName("api_version") val apiVersion: Int = 0,
    val health: Health = Health(),
)

/** Server-sent event on the WebSocket stream. */
@Serializable
data class StreamEvent(
    val type: String = "",
    val decision: Decision? = null,
    val mirror: Mirror? = null,
    val reason: String? = null,
)

@Serializable
data class ValueStats(
    val decisions: Int = 0,
    @SerialName("not_at_computer") val notAtComputer: Int = 0,
    @SerialName("seconds_saved") val secondsSaved: Int = 0,
    @SerialName("baseline_seconds") val baselineSeconds: Int = 90,
)

@Serializable
data class ValueResponse(val week: ValueStats = ValueStats())

/**
 * One session as the escape hatch needs it: what is on screen, and what may be
 * done with it.
 *
 * The capabilities are decided by the computer, from the same key table the
 * decision cards use. The app deliberately does not work them out itself — two
 * places deciding what is allowed is two places that can disagree, and this is
 * the one area where being wrong types a sentence into somebody's menu.
 */
@Serializable
data class SessionView(
    @SerialName("host_id") val hostId: String = "",
    @SerialName("host_label") val hostLabel: String = "",
    @SerialName("pane_id") val paneId: String = "",
    @SerialName("workspace_id") val workspaceId: String = "",
    @SerialName("workspace_label") val workspaceLabel: String = "",
    val agent: String = "",
    @SerialName("display_agent") val displayAgent: String = "",
    @SerialName("agent_status") val agentStatus: String = WireValues.AgentStatus.UNKNOWN,
    val title: String = "",
    val text: String = "",
    val source: String = "recent",
    val lines: Int = 0,
    val revision: Long = 0,
    val truncated: Boolean = false,
    val rule: String = "",
    @SerialName("decision_id") val decisionId: String = "",
    val approvable: Boolean = false,
    val rejectable: Boolean = false,
    val answerable: Boolean = false,
    @SerialName("can_deliver_text") val canDeliverText: Boolean = true,
    @SerialName("setup_required") val setupRequired: Boolean = false,
    @SerialName("setup_reason") val setupReason: String = "",
    @SerialName("setup_fix") val setupFix: String = "",
    val unsent: String = "",
    @SerialName("unsent_command") val unsentCommand: Boolean = false,
    /**
     * True when the bottom of this session is a *menu* rather than an input line.
     *
     * codex's model picker is the case that produced this field (2026-10-13): typing into
     * it drives the menu — a sentence sent while it was open advanced it to the next
     * question — and herdr reports such a pane as `idle`, so nothing else in the system
     * knows. Sending from the app is refused while this is true; the keys are still there
     * for the person to press.
     */
    val menu: Boolean = false,
    val status: String = "",
    /**
     * How many trailing lines of [text] are the harness's own furniture (its input box,
     * the rules around it, its footer). [conversation] drops them; the terminal view keeps
     * everything.
     */
    @SerialName("chrome_lines") val chromeLines: Int = 0,
    val unavailable: String = "",
) {
    val agentLabel: String get() = displayAgent.ifBlank { agent.ifBlank { "this session" } }

    val conversation: String
        get() = dev.perch.console.core.format.SessionBody.of(text, chromeLines)

    /**
     * True when there is no coding agent in this session — a shell, a server, a
     * test runner.
     *
     * The distinction is not cosmetic: it changes what sending text *means*. A
     * shell that receives a line break runs the line, with no Enter from us, and
     * that is worth saying on screen before the user finds out by sending a
     * two-line sentence (measured: scripts/verify-us3.sh).
     */
    val hasNoAgent: Boolean get() = agent.isBlank() && displayAgent.isBlank()

    val waitingOnAPerson: Boolean
        get() = AgentStates.needsDecision(agentStatus) || decisionId.isNotBlank()
}

/** Error body the bridge returns on every failed request. */
@Serializable
data class ErrorBody(
    val error: String = "",
    val fix: String = "",
)

// ---------------------------------------------------------------- status helpers

typealias AgentStates = WireValues.AgentStatus
