package dev.perch.console

import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.Transcript
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The session menu's doors, held open.
 *
 * This exists because of a defect that no other check could see (2026-10-15): the rename and close-session
 * actions were built — dialogs, callbacks, tests, the lot — and the menu never grew the two items that open
 * them. Nothing failed. There was simply no way to reach the feature from the app, and every unit test in the
 * project passed, because they all tested the things that *were* there.
 *
 * A walk of the app on the emulator found it in one minute. This test is the cheaper version of that walk: it
 * asserts the menu actually offers the doors, and that opening one reaches the dialog.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class SessionMenuTest {

    @get:Rule
    val compose = createComposeRule()

    private val session = SessionView(
        paneId = "wA:p1",
        hostId = "local",
        hostLabel = "dev-laptop",
        workspaceLabel = "mobile agent",
        agent = "codex",
        displayAgent = "codex",
        agentStatus = "idle",
        title = "the session's own name",
        text = "an answer",
    )

    @androidx.compose.runtime.Composable
    private fun screen(
        onRename: (String) -> Unit = {},
        onEndSession: () -> Unit = {},
    ) {
        Box(modifier = Modifier.fillMaxSize()) {
            SessionScreen(
                session = session,
                busy = false,
                onApprove = {},
                onReject = {},
                onSend = {},
                onRefresh = {},
                onClose = {},
                conversation = Transcript(readable = true),
                onRename = onRename,
                onEndSession = onEndSession,
            )
        }
    }

    @Test
    fun theMenuOffersNamingAndEndingTheSession() {
        compose.setContent { PerchTheme(dynamicColour = false) { screen() } }
        compose.onNodeWithContentDescription("Menu").performClick()

        // Both words are the product's, and both are the only way to reach the dialogs behind them.
        compose.onNodeWithText("Rename…").assertIsDisplayed()
        compose.onNodeWithText("Close session").assertIsDisplayed()
        // The screen's own door is still there and still means "leave this screen", not "end the session".
        compose.onNodeWithText("Close").assertIsDisplayed()
    }

    /*
     * There is deliberately no test here that clicks "Rename…" and reads the dialog it opens.
     *
     * It cannot be written in this harness: the dialog's text field takes focus, a focused text field blinks
     * its cursor for ever, and Compose therefore never reports "idle" — the assertion after the click hangs
     * for sixty seconds and fails with `AppNotIdleException`, which says nothing about the code. The same wall
     * pushed the warning logic into `ManageCopy`, where it is tested in every combination.
     *
     * What stands in for it: `ManageDialogsTest` renders the dialog directly (no focus, no blink) and checks
     * the field starts at the current name and that Save is dead while the name is empty; and the emulator
     * walk of 2026-10-15 showed the real dialog on a real Android, prefilled with the project's name, with
     * the hint and both buttons where they belong.
     */

    @Test
    fun theMenusCloseItemAsksBeforeEndingAnything() {
        var ended = false
        compose.setContent { PerchTheme(dynamicColour = false) { screen(onEndSession = { ended = true }) } }
        compose.onNodeWithContentDescription("Menu").performClick()
        compose.onNodeWithText("Close session").performClick()

        // The dialog is what stands between a tap and somebody's running work: opening it must not end anything.
        compose.onNodeWithText("Close this session?").assertIsDisplayed()
        assertEquals(false, ended)
    }
}
