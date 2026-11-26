package dev.perch.console

import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.hasScrollAction
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.test.performTouchInput
import androidx.compose.ui.test.swipeDown
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertTrue
import org.junit.Assert.assertFalse
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class SessionLivenessTest {

    @get:Rule
    val compose = createComposeRule()

    private val session = SessionView(
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agent = "codex",
        displayAgent = "codex",
        agentStatus = "idle",
        text = "> do the thing",
    )

    @androidx.compose.runtime.Composable
    private fun screen(onVisible: (Boolean) -> Unit) {
        SessionScreen(
            session = session,
            busy = false,
            onApprove = {},
            onReject = {},
            onSend = {},
            onRefresh = {},
            onClose = {},
            onVisible = onVisible,
        )
    }

    @Test
    fun anOpenSessionIsBeingLookedAt() {
        val seen = mutableListOf<Boolean>()
        var open by mutableStateOf(false)

        compose.setContent {
            PerchTheme {
                if (open) screen { seen += it }
            }
        }
        // Settle first: the point is that the screen arrives *after* the activity is
        // already started, exactly as it does when the user taps a session.
        compose.waitForIdle()
        open = true
        compose.waitForIdle()

        assertTrue(
            "a session opened onto a running screen must say it is being looked at, " +
                "or nothing keeps it live: $seen",
            seen.contains(true),
        )
    }

    @Test
    fun aClosedSessionIsNot() {
        val seen = mutableListOf<Boolean>()
        var open by mutableStateOf(true)

        compose.setContent {
            PerchTheme {
                if (open) screen { seen += it }
            }
        }
        compose.waitForIdle()
        open = false
        compose.waitForIdle()

        assertTrue(
            "the last thing a closed session says must be that it is closed: $seen",
            seen.lastOrNull() == false,
        )
    }

    @Test
    fun readingUpIsAllowedAndTheWayBackIsOneTap() {
        val following = mutableListOf<Boolean>()
        val long = (1..120).joinToString("\n") { "line $it of a long answer" }

        compose.setContent {
            PerchTheme {
                SessionScreen(
                    session = session.copy(text = long),
                    busy = false,
                    onApprove = {},
                    onReject = {},
                    onSend = {},
                    onRefresh = {},
                    onClose = {},
                    onFollowing = { following += it },
                )
            }
        }
        compose.waitForIdle()
        assertTrue("opening a session starts at its end: $following", following.last())

        // The finger goes on the text, not on "a scrollable" — because there are two now: the reading area
        // scrolls down the page, and the *screen text* fallback scrolls sideways (a screen must not be
        // re-wrapped, see ScreenText). A swipe over the text is what a person does, and the nested scroll
        // sends the vertical part to the page and the sideways part to the lines.
        compose.onNodeWithText("line 1 of a long answer", substring = true)
            .performTouchInput { swipeDown() }
        compose.waitForIdle()

        assertFalse("a finger moving up the page is a reader, not a follower: $following", following.last())
        compose.onNodeWithText("Jump to latest").assertExists()

        compose.onNodeWithText("Jump to latest").performClick()
        compose.waitForIdle()

        assertTrue("the button is the way back to following: $following", following.last())
        assertTrue(
            "with following back on, the button has nothing to say",
            compose.onAllNodesWithText("Jump to latest").fetchSemanticsNodes().isEmpty(),
        )
    }
}
