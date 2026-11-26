package dev.perch.console

import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class SessionSetupTest {

    @get:Rule
    val compose = createComposeRule()

    private val session = SessionView(
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agent = "dsh-tui",
        displayAgent = "dsh-tui",
        agentStatus = "idle",
        text = "Welcome to dsh-TUI · Step 1 of 4",
        setupRequired = true,
        setupReason = "dsh-tui is waiting in its first-run setup wizard.",
        setupFix = "Finish the setup steps in the terminal, then try sending again.",
        canDeliverText = false,
    )

    @Test
    fun aSetupPaneExplainsWhatToDoAndOpensTheTerminal() {
        compose.setContent {
            PerchTheme {
                SessionScreen(
                    session = session,
                    busy = false,
                    onApprove = {},
                    onReject = {},
                    onSend = {},
                    onRefresh = {},
                    onClose = {},
                )
            }
        }

        compose.onNodeWithText("This agent is still being set up").assertExists()
        compose.onNodeWithText("dsh-tui is waiting in its first-run setup wizard.").assertExists()
        compose.onNodeWithText("Finish the setup steps in the terminal, then try sending again.")
            .assertExists()
        compose.onNodeWithText("Open terminal").performClick()
        compose.waitForIdle()
        compose.onNodeWithText("Terminal · dsh-tui").assertExists()
    }

    @Test
    fun aSetupPaneHasNoComposer() {
        compose.setContent {
            PerchTheme {
                SessionScreen(
                    session = session,
                    busy = false,
                    onApprove = {},
                    onReject = {},
                    onSend = {},
                    onRefresh = {},
                    onClose = {},
                )
            }
        }

        compose.onNode(hasSetTextAction()).assertDoesNotExist()
        compose.onNodeWithText("Send").assertDoesNotExist()
    }

    @Test
    fun aReadyPaneKeepsItsComposer() {
        compose.setContent {
            PerchTheme {
                SessionScreen(
                    session = session.copy(
                        text = "normal output",
                        setupRequired = false,
                        setupReason = "",
                        setupFix = "",
                        canDeliverText = true,
                    ),
                    busy = false,
                    onApprove = {},
                    onReject = {},
                    onSend = {},
                    onRefresh = {},
                    onClose = {},
                )
            }
        }

        compose.onNode(hasSetTextAction()).assertExists()
        compose.onNodeWithText("Send").assertExists()
    }
}
