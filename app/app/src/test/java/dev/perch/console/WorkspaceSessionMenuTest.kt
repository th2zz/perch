package dev.perch.console

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Pane
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class WorkspaceSessionMenuTest {
    @get:Rule
    val compose = createComposeRule()

    private fun openProject(onEndSession: (String, String) -> Unit) {
        val state = ConsoleState(
            phase = ConsoleState.Phase.Ready,
            mirror = Mirror(
                hosts = listOf(Host(hostId = "remote")),
                workspaces = listOf(Workspace(workspaceId = "w1", label = "two sessions", hostId = "remote")),
                panes = listOf(
                    Pane(paneId = "w1:p1", workspaceId = "w1", hostId = "local", agent = "codex"),
                    Pane(paneId = "w1:p1", workspaceId = "w1", hostId = "remote", agent = "codex", agentStatus = "working"),
                    Pane(paneId = "w1:p2", workspaceId = "w1", hostId = "remote", agent = "claude", agentStatus = "idle"),
                ),
            ),
        )
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(state = state, actions = ConsoleActions(endSession = onEndSession))
            }
        }
        compose.onNodeWithText("Projects").performClick()
        compose.onNodeWithText("two sessions").performClick()
    }

    @Test
    fun closingFromAProjectConfirmsAndTargetsOnlyTheSelectedSession() {
        val ended = mutableListOf<Pair<String, String>>()
        openProject { pane, host -> ended += pane to host }
        compose.onAllNodesWithText("Close session")[0].performClick()
        compose.onNodeWithText("Close this session?").assertIsDisplayed()
        assertEquals(emptyList<Pair<String, String>>(), ended)
        compose.onNodeWithText("Close it").performClick()
        assertEquals(listOf("w1:p1" to "remote"), ended)
    }

    @Test
    fun cancellingLeavesBothSessionsAlone() {
        val ended = mutableListOf<Pair<String, String>>()
        openProject { pane, host -> ended += pane to host }
        compose.onAllNodesWithText("Close session")[1].performClick()
        compose.onNodeWithText("Cancel").performClick()
        assertEquals(emptyList<Pair<String, String>>(), ended)
        compose.onNodeWithText("Close this session?").assertDoesNotExist()
    }
}
