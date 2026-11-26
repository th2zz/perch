package dev.perch.console

import android.Manifest
import android.app.Application
import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Workspace
import dev.perch.console.core.live.ConnectionStatus
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.features.queue.PerchNotifications
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * The failure mode that looks like success.
 *
 * A user who denied the notification prompt — or turned Perch's notifications off in
 * Settings months later — gets exactly the product they did not pay for: the agent
 * waits on their computer, the phone stays silent, and every screen in the app still
 * looks fine. Nothing about that is visible unless the app says it, which is what
 * spec FR-015 requires of every state ("a reason and a next step"), and what this
 * test pins in both directions.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class NotificationBlockedTest {

    @get:Rule
    val compose = createComposeRule()

    private val blockedCopy = "Notifications are off for Perch, so a waiting agent cannot reach you."

    private fun showConsole(notificationsBlocked: Boolean) {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(
                        phase = ConsoleState.Phase.Ready,
                        connection = ConnectionStatus.Live,
                        mirror = Mirror(
                            hosts = listOf(Host(hostId = "local", label = "dev-laptop")),
                            workspaces = listOf(Workspace(workspaceId = "wA", label = "mobile agent")),
                        ),
                        notificationsBlocked = notificationsBlocked,
                    ),
                    actions = ConsoleActions(),
                )
            }
        }
    }

    @Test
    fun itSaysSoWhenThePhoneWillNotDeliverNotifications() {
        showConsole(notificationsBlocked = true)
        compose.onNodeWithText(blockedCopy).assertIsDisplayed()
        // And it carries the way out, rather than leaving the user to hunt for it.
        compose.onNodeWithText("Turn them back on").assertIsDisplayed()
    }

    @Test
    fun itSaysNothingWhenTheyAreOn() {
        showConsole(notificationsBlocked = false)
        compose.onAllNodesWithText(blockedCopy).assertCountEquals(0)
        compose.onAllNodesWithText("Turn them back on").assertCountEquals(0)
    }

    /**
     * The flag has to come from Android, not from a guess: the same call decides
     * whether the background service may post a notification at all.
     */
    @Test
    fun theAppsOwnPermissionCheckAgreesWithTheScreensState() {
        val app = ApplicationProvider.getApplicationContext<Application>()
        shadowOf(app).denyPermissions(Manifest.permission.POST_NOTIFICATIONS)
        assertFalse(
            "with the permission denied, the app must not believe it can notify",
            PerchNotifications.allowed(app),
        )
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)
        assertTrue(
            "with the permission granted, nothing should be reported as blocked",
            PerchNotifications.allowed(app),
        )
    }
}
