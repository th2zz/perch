package dev.perch.console

import androidx.activity.ComponentActivity
import androidx.compose.ui.test.junit4.createAndroidComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.core.live.ConnectionStatus
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class ProjectBackTest {

    @get:Rule
    val compose = createAndroidComposeRule<ComponentActivity>()

    private val state = ConsoleState(
        phase = ConsoleState.Phase.Ready,
        mirror = Mirror(
            workspaces = listOf(
                Workspace(workspaceId = "w1", number = 1, label = "mobile agent", paneCount = 1),
            ),
        ),
        connection = ConnectionStatus.Live,
    )

    private fun openProjects() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(state = state, actions = ConsoleActions())
            }
        }
        compose.waitForIdle()
        compose.onNodeWithText("Projects").performClick()
        compose.waitForIdle()
    }

    @Test
    fun backInsideAProjectReturnsToTheProjectList() {
        openProjects()
        compose.onNodeWithText("mobile agent").performClick()
        compose.waitForIdle()
        compose.onNodeWithText("‹ Projects").assertExists()

        compose.runOnUiThread { compose.activity.onBackPressedDispatcher.onBackPressed() }
        compose.waitForIdle()

        compose.onNodeWithText("‹ Projects").assertDoesNotExist()
    }

    @Test
    fun backAtTheProjectListIsNotSwallowed() {
        openProjects()
        compose.onNodeWithText("‹ Projects").assertDoesNotExist()

        compose.runOnUiThread { compose.activity.onBackPressedDispatcher.onBackPressed() }
        compose.waitForIdle()

        assertTrue("back at the root must finish (minimise) the app", compose.activity.isFinishing)
    }
}
