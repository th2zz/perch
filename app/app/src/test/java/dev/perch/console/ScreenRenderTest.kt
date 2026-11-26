package dev.perch.console

import android.graphics.Bitmap
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.assertIsNotDisplayed
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.ValueStats
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Pane
import dev.perch.console.core.live.ConnectionStatus
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.features.insight.WeekSummary
import dev.perch.console.features.onboarding.InviteScreen
import dev.perch.console.features.onboarding.OnboardingScreen
import dev.perch.console.features.queue.QueuePage
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * Renders the screens and writes them out as images.
 *
 * Everything in this app has been reasoned about and tested in pieces, and never
 * once looked at. Layout mistakes do not announce themselves: text that overflows
 * its card, a row that pushes a button off the screen, a label in a colour nobody
 * can read, a string that was never wired up and renders as an empty gap. Those
 * are invisible to unit tests and obvious in a picture.
 *
 * `@GraphicsMode(NATIVE)` matters: Robolectric's default graphics mode does not
 * rasterise, so every screen would come back blank and the "test" would pass
 * while showing nothing.
 *
 * The images land in app/build/screenshots/ — look at them after a change to a
 * screen. This is not a substitute for holding the phone; it is what stops the
 * phone being the first place anyone notices a layout is broken.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
// Pinned, not inherited: Robolectric needs an instrumented image of the exact API
// level, and chasing the newest one is not the point of a layout check.
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class ScreenRenderTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    private fun capture(name: String, content: @androidx.compose.runtime.Composable () -> Unit) {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) { content() }
            }
        }
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "$name.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
        println("wrote ${File(outputDir, "$name.png").absolutePath}")
    }

    private fun waitingDecision(
        approvable: Boolean = true,
        rejectable: Boolean = true,
        answerable: Boolean = false,
    ) = Decision(
        id = "wA:p1@1",
        paneId = "wA:p1",
        workspaceLabel = "perch",
        agentKind = "claude",
        displayAgent = "claude",
        rule = "bash_permission_prompt",
        // Laid out the way Claude actually draws it: what it wants to run, then
        // the question, then the options. The order matters, because the headline
        // is derived from it.
        promptText = "Bash command\n" +
            "  npm run test:integration -- --since main\n" +
            "Do you want to proceed?\n" +
            "❯ 1. Yes\n  2. Yes, and don't ask again\n  3. No",
        approvable = approvable,
        rejectable = rejectable,
        answerable = answerable,
        createdAt = "2026-10-13T08:00:00Z",
    )

    @Test
    fun theQueue() {
        capture("queue") {
            QueuePage(
                waiting = listOf(waitingDecision()),
                busyDecisionId = null,
                onDecide = { _, _ -> },
                onAnswer = { },
                onOpenSession = { },
            )
        }
    }

    @Test
    fun theQuestionWithMarkdownInItIsRendered() {
        val decision = waitingDecision().copy(
            promptText = "Bash command\n" +
                "  `rm -rf ./build`\n" +
                "**Do you want to proceed?**\n" +
                "❯ 1. Yes, and **remember** this\n" +
                "  2. No\n" +
                "──────\n" +
                "- it deletes only `./build`\n" +
                "- it runs in *this* workspace",
        )
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
                    QueuePage(
                        waiting = listOf(decision),
                        busyDecisionId = null,
                        onDecide = { _, _ -> },
                        onAnswer = { },
                        onOpenSession = { },
                    )
                }
            }
        }
        compose.waitForIdle()
        // Open the excerpt so the rendered body is on screen.
        compose.onNodeWithText(javaClass.getResource("")?.toString()?.let { "What it asked" } ?: "What it asked")
            .performClick()
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "queue-markdown.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    @Test
    fun theQueueWhenNothingCanBePressed() {
        // The state the product's honesty rule produces most often. It has to read
        // as an explanation rather than as a broken card.
        capture("queue-unmappable") {
            QueuePage(
                waiting = listOf(
                    waitingDecision(approvable = false, rejectable = false).copy(
                        // The real wording the bridge sends, not a paraphrase: a
                        // fixture that reads better than the product hides exactly
                        // the problem these images exist to catch.
                        unmappableReason = "Only a window title shows that something " +
                            "needs you; the dialog itself is not visible.",
                    ),
                ),
                busyDecisionId = null,
                onDecide = { _, _ -> },
                onAnswer = { },
                onOpenSession = { },
            )
        }
    }

    @Test
    fun theQueueWhenNothingIsWaiting() {
        capture("queue-empty") {
            QueuePage(
                waiting = emptyList(),
                busyDecisionId = null,
                onDecide = { _, _ -> },
                onAnswer = { },
                onOpenSession = { },
            )
        }
    }

    @Test
    fun aQueueWithSeveralRequestsOnIt() {
        // The layout is only proven by the case it was not designed around: five
        // cards at once, with the week's totals underneath. If something is going to
        // push a button off the screen, this is where it happens.
        capture("queue-many") {
            QueuePage(
                waiting = listOf(
                    waitingDecision().copy(id = "a", workspaceLabel = "perch"),
                    waitingDecision(approvable = true, rejectable = false).copy(
                        id = "b", workspaceLabel = "backend", agentKind = "codex", displayAgent = "codex",
                        promptText = "Trust this folder?\n❯ 1. Trust\n  2. No",
                    ),
                    waitingDecision(approvable = false, rejectable = false, answerable = true).copy(
                        id = "c", workspaceLabel = "cli-tools", agentKind = "hermes", displayAgent = "hermes",
                        promptText = "Which environment should I deploy to?",
                    ),
                    waitingDecision(approvable = false, rejectable = false).copy(
                        id = "d", workspaceLabel = "docs-site", agentKind = "gemini", displayAgent = "gemini",
                        unmappableReason = "This Gemini prompt has several variants with different defaults, " +
                            "so Perch will not pick one for you.",
                    ),
                    waitingDecision().copy(id = "e", workspaceLabel = "infra"),
                ),
                busyDecisionId = null,
                onDecide = { _, _ -> },
                onAnswer = { },
                onOpenSession = { },
            )
        }
    }

    @Test
    fun aRequestWhoseTextIsVeryLong() {
        // The collapsed card clamps to two lines. This checks what it clamps to,
        // and that the buttons are still reachable underneath.
        capture("queue-long-prompt") {
            QueuePage(
                waiting = listOf(
                    waitingDecision().copy(
                        promptText = (1..12).joinToString("\n") { "line $it of a very talkative agent" } +
                            "\nDo you want to proceed?\n❯ 1. Yes\n  2. No",
                    ),
                ),
                busyDecisionId = null,
                onDecide = { _, _ -> },
                onAnswer = { },
                onOpenSession = { },
            )
        }
    }

    @Test
    fun theVoiceSettingsOpen() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                dev.perch.console.features.compose.VoiceSetup(
                    configured = false,
                    onSave = { _, _ -> },
                )
            }
        }
        // Folded away by default, so the render has to open it. Rendering the
        // collapsed state and calling it "the voice settings" is how the first
        // version of this screenshot showed a single line and nothing else.
        compose.onNodeWithText("Voice input · off").performClick()
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "voice-settings.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }



    @Test
    fun theUsagePanel() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
                    SessionScreen(
                        session = SessionView(
                            paneId = "wA:p1",
                            hostId = "local",
                            hostLabel = "dev-laptop",
                            workspaceLabel = "perch",
                            agent = "codex",
                            displayAgent = "codex",
                            agentStatus = "working",
                            text = "### Summary\n\nAll integration checks passed.",
                        ),
                        busy = false,
                        onApprove = { },
                        onReject = { },
                        onSend = { },
                        onRefresh = { },
                        onClose = { },
                        usage = dev.perch.console.core.herdrclient.UsageFacts(
                            harness = "codex",
                            model = "codex-default",
                            effort = "high",
                            contextUsed = 128_400,
                            contextWindow = 200_000,
                            totalTokens = 1_540_000,
                            cachedTokens = 1_210_000,
                            access = "workspace-write, ask on untrusted commands",
                            goal = "Implement reconnect backoff and verify contract tests.",
                            goalStatus = "in_progress",
                        ),
                    )
                }
            }
        }
        compose.waitForIdle()
        compose.onNodeWithContentDescription("Menu").performClick()
        compose.onNodeWithText("Usage").performClick()
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "session-usage.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    @Test
    fun theConversationEndsAtTheAnswerNotAtTheTerminalFurniture() {
        capture("session-body") {
            SessionScreen(
                session = SessionView(
                    paneId = "wA:p1",
                    hostId = "local",
                    hostLabel = "dev-laptop",
                    workspaceLabel = "perch",
                    agent = "codex",
                    displayAgent = "codex",
                    agentStatus = "working",
                    title = "Fix terminal viewport scroll",
                    text = "### Done\n" +
                        "\n" +
                        "Updated **TerminalPageView.kt** to preserve cursor visibility when the keyboard opens.\n" +
                        "\n" +
                        "\n" +
                        "› Ask Codex to do anything\n" +
                        "\n" +
                        "  codex high · Context 44% used · 1.2M used\n" +
                        "  ← for agents · ? for shortcuts\n",
                    chromeLines = 4,
                    status = "codex high · Context 44% used · 1.2M used\n" +
                        "← for agents · ? for shortcuts",
                ),
                busy = false,
                onApprove = { },
                onReject = { },
                onSend = { },
                onRefresh = { },
                onClose = { },
            )
        }
    }

    @Test
    fun theConversationWithATableInIt() {
        capture("session-table") {
            SessionScreen(
                session = SessionView(
                    paneId = "wF:p1",
                    hostId = "local",
                    hostLabel = "dev-laptop",
                    workspaceLabel = "networking",
                    agent = "agy",
                    displayAgent = "agy",
                    agentStatus = "idle",
                    title = "Transport comparison",
                    text = "### Transport Options\n" +
                        "\n" +
                        "Comparing private-network modes for phone-to-host connectivity:\n" +
                        "\n" +
                        " Mode      | Encryption           | Setup" +
                        "                       | Notes\n" +
                        "-----------|----------------------|" +
                        "-----------------------------|----------------------------\n" +
                        " Tailscale | WireGuard end-to-end | Install on both devices" +
                        "     | Works across Wi-Fi and LTE\n" +
                        " Local LAN | Private subnet       | Same Wi-Fi network" +
                        "          | Lowest local latency\n" +
                        "\n" +
                        "### Next section: Pairing\n",
                ),
                busy = false,
                onApprove = { },
                onReject = { },
                onSend = { },
                onRefresh = { },
                onClose = { },
            )
        }
    }

    @Test
    fun theConversationWhenTheTerminalIsHoldingText() {
        capture("session-unsent") {
            SessionScreen(
                session = SessionView(
                    paneId = "wA:p1",
                    hostId = "local",
                    hostLabel = "dev-laptop",
                    workspaceLabel = "perch",
                    agent = "agy",
                    displayAgent = "agy",
                    agentStatus = "idle",
                    title = "Bridge protocol review",
                    text = "### Summary\n\nUpdated **contracts/api-shape.json**:\n\n- Added reconnect backoff fields\n- Verified Go and Kotlin decoders match\n",
                    unsent = "Can you also add a unit test for stale host snapshots before merging?",
                ),
                busy = false,
                onApprove = { },
                onReject = { },
                onSend = { },
                onRefresh = { },
                onClose = { },
            )
        }
    }

    @Test
    fun theWeekSummary() {
        capture("week") {
            WeekSummary(
                stats = ValueStats(
                    decisions = 9,
                    notAtComputer = 7,
                    secondsSaved = 630,
                    baselineSeconds = 90,
                ),
            )
        }
    }

    @Test
    fun theWeekSummaryBeforeAnythingHasHappened() {
        capture("week-empty") {
            WeekSummary(stats = ValueStats())
        }
    }

    @Test
    fun oneProjectOpened() {
        // Reached by tapping a project, so the render taps it too.
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(
                        phase = ConsoleState.Phase.Ready,
                        mirror = mirror(),
                        connection = ConnectionStatus.Live,
                    ),
                    actions = ConsoleActions(),
                )
            }
        }
        compose.onNodeWithText("Projects").performClick()
        compose.waitForIdle()
        compose.onNodeWithText("perch").performClick()
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "workspace-detail.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    // ------------------------------------------------------------ first run

    @Test
    fun theFirstScreenAnyoneSees() {
        // The screen where the product is either made or lost. It has never been
        // looked at, and its copy was rewritten twice.
        capture("onboarding") {
            OnboardingScreen(busy = false, error = null, onPair = { _, _ -> })
        }
    }

    @Test
    fun theFirstScreenWithABadAddress() {
        capture("onboarding-error") {
            OnboardingScreen(
                busy = false,
                error = "That address does not look right. It should be like 100.64.0.1:45999",
                onPair = { _, _ -> },
            )
        }
    }

    @Test
    fun theInviteThatArrivesByLink() {
        capture("invite") {
            InviteScreen(
                invite = dev.perch.console.core.auth.PairInvite(
                    address = "100.64.0.1:45999",
                    token = "…",
                    computerName = "dev-laptop",
                ),
                busy = false,
                error = null,
                onAccept = { },
                onUseManualEntry = { },
            )
        }
    }

    @Test
    fun theReplyBoxWithVoiceSetUp() {
        capture("compose-voice") {
            dev.perch.console.features.queue.ComposeReply(
                decision = waitingDecision(approvable = false, rejectable = false, answerable = true),
                busy = false,
                voiceConfigured = true,
                voiceRecording = false,
                voiceTranscribing = false,
                transcript = null,
                onVoiceStart = { },
                onVoiceStop = { },
                onVoiceDiscard = { },
                onSend = { },
                onDismiss = { },
            )
        }
    }

    @Test
    fun theReplyBoxWhileListening() {
        capture("compose-listening") {
            dev.perch.console.features.queue.ComposeReply(
                decision = waitingDecision(approvable = false, rejectable = false, answerable = true),
                busy = false,
                voiceConfigured = true,
                voiceRecording = true,
                voiceTranscribing = false,
                transcript = null,
                onVoiceStart = { },
                onVoiceStop = { },
                onVoiceDiscard = { },
                onSend = { },
                onDismiss = { },
            )
        }
    }

    // ------------------------------------------------------------ the shell

    private fun mirror() = Mirror(
        hosts = listOf(dev.perch.console.core.herdrclient.Host(hostId = "local", label = "dev-laptop")),
        herdrVersion = "0.9.1",
        protocol = 22,
        workspaces = listOf(
            Workspace(workspaceId = "w2", number = 1, label = "backend", paneCount = 2, agentStatus = "idle"),
            Workspace(workspaceId = "wA", number = 2, label = "perch", paneCount = 2, agentStatus = "blocked"),
            Workspace(workspaceId = "wC", number = 3, label = "cli-tools", paneCount = 1, agentStatus = "working"),
        ),
        panes = listOf(
            Pane(paneId = "wA:p1", workspaceId = "wA", agent = "claude", displayAgent = "claude",
                agentStatus = "blocked", cwd = "/home/user/workspace/perch"),
            Pane(paneId = "wA:p3", workspaceId = "wA", agentStatus = "unknown"),
            Pane(paneId = "w2:p1", workspaceId = "w2", agent = "codex", displayAgent = "codex",
                agentStatus = "idle", terminalTitle = "Refactor worker queue | backend"),
            Pane(paneId = "wC:p1", workspaceId = "wC", agent = "codex", displayAgent = "codex",
                agentStatus = "working", terminalTitle = "Run integration checks | cli-tools"),
        ),
    )

    /**
     * Two computers, one of which did not answer.
     *
     * The ids are identical on both hosts on purpose: that is what two herdr
     * machines look like when neither has been arranged, and it is the case that a
     * screen only listing "w1" would silently merge.
     */
    private fun twoMachineMirror() = Mirror(
        hosts = listOf(
            dev.perch.console.core.herdrclient.Host(
                hostId = "local", label = "dev-laptop", kind = "local", status = "reachable",
            ),
            dev.perch.console.core.herdrclient.Host(
                hostId = "workbox", label = "workbox", kind = "ssh", status = "reachable",
            ),
            dev.perch.console.core.herdrclient.Host(
                hostId = "old-mac", label = "old-mac", kind = "ssh", status = "unreachable",
                reason = "Perch could not reach old-mac.",
                fix = "Check that the computer is on and reachable; Perch will try again on its own.",
            ),
        ),
        herdrVersion = "0.9.1",
        protocol = 22,
        workspaces = listOf(
            Workspace(workspaceId = "w1", number = 1, label = "perch", paneCount = 1, hostId = "local"),
            Workspace(workspaceId = "w1", number = 1, label = "api", paneCount = 1, hostId = "workbox"),
            Workspace(workspaceId = "w2", number = 2, label = "tools", paneCount = 1, hostId = "workbox"),
        ),
        panes = listOf(
            Pane(paneId = "w1:p1", workspaceId = "w1", hostId = "local", agent = "codex",
                displayAgent = "codex", agentStatus = "working"),
            Pane(paneId = "w1:p1", workspaceId = "w1", hostId = "workbox", agent = "claude",
                displayAgent = "claude", agentStatus = "blocked"),
            Pane(paneId = "w2:p1", workspaceId = "w2", hostId = "workbox", agentStatus = "unknown"),
        ),
    )

    /**
     * The project list with more than one computer.
     *
     * What this render is for: the computer name has to be attached to the projects
     * it belongs to, a machine that did not answer has to say so instead of its
     * projects disappearing, and none of it may push a project off the screen.
     */
    @Test
    fun theProjectsOnTwoComputers() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(
                        phase = ConsoleState.Phase.Ready,
                        mirror = twoMachineMirror(),
                        connection = ConnectionStatus.Live,
                    ),
                    actions = ConsoleActions(),
                )
            }
        }
        compose.onNodeWithText("Projects").performClick()
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "projects-two-computers.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    /**
     * The queue with a request that came from another computer.
     *
     * Rendered because the card's title is the only place the phone says which
     * machine it is asking about, and two machines can have projects with the same
     * name.
     */
    @Test
    fun aRequestFromAnotherComputer() {
        capture("queue-other-computer") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = twoMachineMirror(),
                    decisions = listOf(
                        waitingDecision().copy(
                            id = "workbox/w1:p1@1",
                            hostId = "workbox",
                            paneId = "w1:p1",
                            workspaceLabel = "api",
                            approvable = true,
                            rejectable = true,
                        ),
                    ),
                    connection = ConnectionStatus.Live,
                ),
                actions = ConsoleActions(),
            )
        }
    }

    @Test
    fun theConversationSideOfASession() {
        capture("session-conversation") {
            SessionScreen(
                session = SessionView(
                    paneId = "wA:p1",
                    workspaceLabel = "perch",
                    agent = "agy",
                    displayAgent = "agy",
                    agentStatus = "idle",
                    text = "## Architecture notes\n" +
                        "\n" +
                        "The bridge stays **local-first** for two reasons:\n" +
                        "\n" +
                        "- Zero cloud accounts or hosted proxies\n" +
                        "- Fast reconnects over Tailscale or LAN\n" +
                        "\n" +
                        "```sh\n" +
                        "go test ./bridge/...\n" +
                        "```\n" +
                        "────────────────────────────\n" +
                        "See `README.md` for pairing steps.",
                ),
                busy = false,
                onApprove = { },
                onReject = { },
                onSend = { },
                onRefresh = { },
                onClose = { },
            )
        }
        // And the key row is genuinely absent, not merely off-screen: it belongs to
        // the terminal side.
        compose.onNodeWithText("Esc").assertIsNotDisplayed()
    }

    /** The escape hatch on a session that is not on this computer. */
    @Test
    fun theEscapeHatchOnAnotherComputer() {
        capture("session-other-computer") {
            SessionScreen(
                session = SessionView(
                    hostId = "workbox",
                    hostLabel = "workbox",
                    paneId = "w1:p1",
                    workspaceLabel = "api",
                    agent = "claude",
                    displayAgent = "claude",
                    agentStatus = "blocked",
                    text = "Bash(npm test)\n\nDo you want to proceed?\n❯ 1. Yes\n  2. No",
                    approvable = true,
                    rejectable = true,
                ),
                busy = false,
                onApprove = { },
                onReject = { },
                onSend = { },
onRefresh = { },
                onClose = { },
            )
        }
    }

    @Test
    fun theShellWithSomethingWaiting() {
        capture("shell-waiting") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = mirror(),
                    decisions = listOf(waitingDecision()),
                    connection = ConnectionStatus.Live,
                    week = ValueStats(decisions = 9, notAtComputer = 7, secondsSaved = 630, baselineSeconds = 90),
                ),
                actions = ConsoleActions(),
            )
        }
    }

    /**
     * The queue screen when the phone cannot deliver notifications.
     *
     * Rendered because this line is the difference between "the product is broken"
     * and "one setting is off": it has to be legible, in the place a user looks
     * first, and it must not push the queue itself off the screen.
     */
    @Test
    fun theShellWhenNotificationsAreOff() {
        capture("shell-notifications-off") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = mirror(),
                    decisions = listOf(waitingDecision(approvable = true, rejectable = true)),
                    connection = ConnectionStatus.Live,
                    notificationsBlocked = true,
                ),
                actions = ConsoleActions(),
            )
        }
    }

    /**
     * The other silent failure, rendered: a phone whose manufacturer stops
     * background apps. Same screen, same place, so the user learns one place to
     * look when something stops arriving.
     */
    @Test
    fun theShellWhenThePhoneRestrictsBackgroundApps() {
        capture("shell-battery-restricted") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = mirror(),
                    connection = ConnectionStatus.Live,
                    backgroundRestricted = true,
                ),
                actions = ConsoleActions(),
            )
        }
    }

    /**
     * The lines that only appear *after* something happened: a decision the phone
     * just handled, a plain statement like "saved, will send later", and the count
     * of replies still waiting for the computer.
     *
     * Rendered because they are the product's own claims about itself, and they had
     * never been looked at. A confirmation that overlaps the queue, or a count that
     * renders as "1 replies", is exactly the kind of thing that only shows up in a
     * picture.
     */
    @Test
    fun theConfirmationsAndTheWaitingCount() {
        capture("shell-after-a-decision") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = mirror(),
                    connection = ConnectionStatus.Live,
                    recentWin = dev.perch.console.core.format.Duration.savedClaim(90),
                    notice = "Saved. It will go through when your computer answers again.",
                    queuedReplies = 2,
                    week = ValueStats(
                        decisions = 9,
                        notAtComputer = 7,
                        secondsSaved = 630,
                        baselineSeconds = 90,
                    ),
                ),
                actions = ConsoleActions(),
            )
        }
    }

    /**
     * A card whose whole question is one line.
     *
     * The "What it asked" toggle used to appear anyway, and opening it showed the
     * same sentence — a button that does nothing, which is worse than no button.
     * Rendered so the rule ("only if it reveals more") is visible, not just asserted.
     */
    @Test
    fun aCardWhoseQuestionIsAlreadyVisible() {
        capture("queue-question-only") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = mirror(),
                    decisions = listOf(
                        waitingDecision().copy(promptText = "Trust this folder?"),
                    ),
                    connection = ConnectionStatus.Live,
                ),
                actions = ConsoleActions(),
            )
        }
    }

    @Test
    fun theShellWhenTheComputerIsAsleep() {
        // The offline posture: the last known queue is still readable, and the
        // reason plus the next step are on screen rather than implied.
        capture("shell-offline") {
            ConsoleScreen(
                state = ConsoleState(
                    phase = ConsoleState.Phase.Ready,
                    mirror = mirror(),
                    decisions = listOf(waitingDecision()),
                    offline = true,
                    connection = ConnectionStatus.Retrying,
                    problem = ConsoleState.Problem(
                        "Your computer did not answer.",
                        "Anything you wrote has been kept and will go through when your computer answers again.",
                    ),
                ),
                actions = ConsoleActions(),
            )
        }
    }

    @Test
    fun theProjectListWithMoreThanOneComputer() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(
                        phase = ConsoleState.Phase.Ready,
                        mirror = mirror().copy(
                            hosts = mirror().hosts + dev.perch.console.core.herdrclient.Host(
                                hostId = "laptop", label = "work-laptop", kind = "ssh", status = "unknown",
                            ),
                        ),
                        connection = ConnectionStatus.Live,
                        watchInBackground = true,
                    ),
                    actions = ConsoleActions(),
                )
            }
        }
        // The tab is switched by tapping it, rather than by injecting a state that
        // says which tab is open. A screenshot that skipped the tap would not show
        // whether the switch works, which is half of what this screen does.
        compose.onNodeWithText("Projects").performClick()
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "shell-projects.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
