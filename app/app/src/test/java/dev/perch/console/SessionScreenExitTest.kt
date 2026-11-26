package dev.perch.console

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.StateRestorationTester
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performTextInput
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class SessionScreenExitTest {

    @get:Rule
    val compose = createAndroidComposeRule<ComponentActivity>()

    private val session = SessionView(
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agent = "codex",
        displayAgent = "codex",
        agentStatus = "idle",
        text = "an answer worth replying to",
    )

    @androidx.compose.runtime.Composable
    private fun screen(onClose: () -> Unit = {}) {
        SessionScreen(
            session = session,
            busy = false,
            onApprove = {},
            onReject = {},
            onSend = {},
            onRefresh = {},
            onClose = onClose,
        )
    }

    @Test
    fun aHalfWrittenReplySurvivesARotation() {
        val tester = StateRestorationTester(compose)
        tester.setContent { PerchTheme { screen() } }

        compose.onNode(hasSetTextAction()).performTextInput("half a sentence")
        compose.waitForIdle()

        tester.emulateSavedInstanceStateRestore()
        compose.waitForIdle()

        compose.onNodeWithText("half a sentence").assertExists()
    }

    /** The system back gesture leaves the session, because it is a screen, not a panel. */
    @Test
    fun theSystemBackGestureClosesTheScreen() {
        var closes = 0
        compose.setContent { PerchTheme { screen(onClose = { closes += 1 }) } }
        compose.waitForIdle()

        compose.runOnUiThread { compose.activity.onBackPressedDispatcher.onBackPressed() }
        compose.waitForIdle()

        assertEquals("back must leave the session screen", 1, closes)
    }
}
