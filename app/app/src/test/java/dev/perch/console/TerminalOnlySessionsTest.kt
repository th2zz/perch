package dev.perch.console

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.live.BridgeStream
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.features.console.ConsoleViewModel
import dev.perch.console.ui.theme.PerchTheme
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class TerminalOnlySessionsTest {
    @get:Rule
    val compose = createComposeRule()

    @After
    fun stopStream() = BridgeStream.stop()

    @Test
    fun theReleaseFlagOpensTheAppInTerminalOnlyMode() {
        val app = ApplicationProvider.getApplicationContext<PerchApp>()
        assertTrue(BuildConfig.SESSION_SCREEN_ONLY)
        assertTrue(ConsoleViewModel(app).state.value.terminalOnlySessions)
    }

    @Test
    fun everyHarnessUsesTheExistingTerminalAndCloseLeavesTheSessionScreen() {
        val state = androidx.compose.runtime.mutableStateOf(ConsoleState(phase = ConsoleState.Phase.Ready))
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = state.value,
                    actions = ConsoleActions(closeSession = { state.value = state.value.copy(session = null) }),
                )
            }
        }
        for (kind in listOf("codex", "claude", "agy", "dsh-tui", "unrecognized")) {
            compose.runOnIdle {
                state.value = state.value.copy(
                    terminalOnlySessions = true,
                    session = SessionView(paneId = "w1:$kind", agent = kind, displayAgent = kind),
                )
            }
            compose.onNodeWithText("Terminal · $kind").assertIsDisplayed()
            compose.onNodeWithContentDescription("Menu").assertDoesNotExist()
            compose.onNodeWithText("Close").performClick()
            compose.onNodeWithText("Terminal · $kind").assertDoesNotExist()
        }
    }

    @Test
    fun disablingThePreferenceKeepsTheConversationAndItsSessionMenu() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(
                        phase = ConsoleState.Phase.Ready,
                        session = SessionView(paneId = "w1:p1", agent = "codex"),
                    ),
                    actions = ConsoleActions(),
                )
            }
        }
        compose.onNodeWithContentDescription("Menu").assertIsDisplayed()
        compose.onNodeWithText("Terminal · codex").assertDoesNotExist()
    }
}
