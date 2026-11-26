package dev.perch.console

import androidx.compose.ui.test.assertIsNotEnabled
import androidx.compose.ui.test.hasSetTextAction
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTextInput
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class SessionComposerGuardTest {

    @get:Rule
    val compose = createComposeRule()

    private val session = SessionView(
        paneId = "wA:p1",
        workspaceLabel = "perch",
        agent = "codex",
        displayAgent = "codex",
        agentStatus = "idle",
        text = "› partial unsent draft",
    )

    @androidx.compose.runtime.Composable
    private fun screen(unsent: String, onSend: (String) -> Unit) {
        SessionScreen(
            session = session.copy(unsent = unsent),
            busy = false,
            onApprove = {},
            onReject = {},
            onSend = onSend,
            onRefresh = {},
            onClose = {},
        )
    }

    @Test
    fun textAlreadyInTheTerminalIsShownAndSendingAsksFirst() {
        var sent: String? = null
        compose.setContent { PerchTheme { screen(unsent = "partial unsent draft") { sent = it } } }

        compose.onNode(hasSetTextAction()).performTextInput("my new sentence")
        compose.onNodeWithText("Send").performClick()
        compose.waitForIdle()

        assertNull("nothing may be sent while the terminal is holding text", sent)
        // The confirmation has to be readable on its own: what is in the terminal, and
        // what is about to be appended to it.
        compose.onNodeWithText("Send anyway").assertExists()
        compose.onNodeWithText("The terminal already has text").assertExists()

        compose.onNodeWithText("Send anyway").performClick()
        compose.waitForIdle()

        assertEquals("my new sentence", sent)
    }

    @Test
    fun anEmptyInputLineSendsInOneTap() {
        var sent: String? = null
        compose.setContent { PerchTheme { screen(unsent = "") { sent = it } } }

        compose.onNode(hasSetTextAction()).performTextInput("my new sentence")
        compose.onNodeWithText("Send").performClick()
        compose.waitForIdle()

        assertEquals(
            "the ordinary case must not grow a second tap",
            "my new sentence",
            sent,
        )
    }

    @Test
    fun nothingIsSentWhileAMenuIsOpen() {
        var sent: String? = null
        compose.setContent {
            PerchTheme {
                SessionScreen(
                    session = session.copy(menu = true),
                    busy = false,
                    onApprove = {},
                    onReject = {},
                    onSend = { sent = it },
                    onRefresh = {},
                    onClose = {},
                )
            }
        }

        compose.onNode(hasSetTextAction()).performTextInput("my new sentence")
        compose.onNodeWithText("Send").assertIsNotEnabled()
        compose.onNodeWithText("Send").performClick()
        compose.waitForIdle()

        assertNull("a menu must not be typed into, even by accident", sent)
    }
}
