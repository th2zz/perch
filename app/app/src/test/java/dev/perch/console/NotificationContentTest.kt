package dev.perch.console

import android.app.Application
import android.app.Notification
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.features.queue.PerchNotifications
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * The notification is the product's front door, and FR-003 is a statement about it:
 * the user must be able to tell which project, which agent, and what is being asked,
 * from the lock screen, without opening anything.
 *
 * The pieces of that sentence were tested separately (the card's labels, the excerpt
 * trimming); this checks the object the user actually sees — title, body, the buttons
 * that appear, and what a tap on them carries.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class NotificationContentTest {

    private val context: Application = ApplicationProvider.getApplicationContext()

    private fun decision(
        approvable: Boolean = true,
        rejectable: Boolean = true,
        hostId: String = "local",
        promptText: String = "Do you want to proceed?\n❯ 1. Yes\n  2. No",
    ) = Decision(
        id = "wA:p1@1",
        hostId = hostId,
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agentKind = "claude",
        displayAgent = "claude",
        promptText = promptText,
        approvable = approvable,
        rejectable = rejectable,
    )

    private fun notificationOf(decision: Decision, waiting: Int = 1): Notification =
        PerchNotifications.decision(context, decision, waiting)

    @Test
    fun reShowingTheSameRequestDoesNotAlertASecondTime() {
        // The watching service keeps the shade equal to the queue, so this card is
        // re-posted whenever anything on the computer moves — that is deliberate
        // (a swiped-away card comes back while the request is still open). What
        // would be wrong is the *buzz*: Android alerts again on every post of a
        // notification that does not carry FLAG_ONLY_ALERT_ONCE, and this card
        // re-stamps `setWhen`, so it even looks like a new arrival. The user would
        // be told "something new" for a request they have already seen, every time
        // an unrelated agent finishes — and would learn to distrust the alerts,
        // which is the one thing this whole product is.
        val flags = notificationOf(decision()).flags
        assertTrue(
            "re-posting an unchanged request must not alert again (FLAG_ONLY_ALERT_ONCE)",
            flags and Notification.FLAG_ONLY_ALERT_ONCE != 0,
        )
    }

    @Test
    fun markdownReachesTheLockScreenAsWordsNotPunctuation() {
        val decision = decision(promptText = "**Do you want** to `proceed`?\n❯ 1. Yes\n  2. No")
        val extras = notificationOf(decision).extras
        val title = extras.getCharSequence(Notification.EXTRA_TITLE).toString()
        val text = extras.getCharSequence(Notification.EXTRA_TEXT).toString()
        val big = extras.getCharSequence(Notification.EXTRA_BIG_TEXT)?.toString().orEmpty()

        for (shown in listOf(title, text, big)) {
            assertFalse("no markdown markers on the lock screen: $shown", shown.contains("**"))
            assertFalse("no markdown markers on the lock screen: $shown", shown.contains("`"))
        }
        assertTrue("the words must survive: $text", text.contains("Do you want"))
    }

    @Test
    fun itNamesTheAgentTheProjectAndTheQuestion() {
        val extras = notificationOf(decision()).extras
        val title = extras.getCharSequence(Notification.EXTRA_TITLE).toString()
        val text = extras.getCharSequence(Notification.EXTRA_TEXT).toString()

        assertTrue("the agent must be named: $title", title.contains("claude"))
        assertTrue("the project must be named: $title", title.contains("mobile agent"))
        // The question, not the last option: a notification that reads "2. No" tells
        // the user nothing about what is being asked.
        assertTrue("the question must be visible: $text", text.contains("proceed"))
        assertFalse("the options must not be what the card leads with", text.startsWith("❯ 1."))
    }

    @Test
    fun anotherComputerIsNamedOnTheNotificationItself() {
        // Two machines can have a project with the same name, and this is the moment
        // the user decides something about one of them (spec FR-016).
        val title = notificationOf(decision(hostId = "workbox")).extras
            .getCharSequence(Notification.EXTRA_TITLE).toString()
        assertTrue("the computer must be named: $title", title.contains("workbox"))
        assertEquals(
            "localhost noise: a local request must not name its computer",
            false,
            notificationOf(decision(hostId = "local")).extras
                .getCharSequence(Notification.EXTRA_TITLE).toString().contains("local"),
        )
    }

    @Test
    fun theButtonsAreTheOnesTheComputerSaidWouldWork() {
        val both = notificationOf(decision())
        assertEquals(2, both.actions.size)

        // A button that leads to a refusal is worse than no button: it teaches the
        // user that tapping does not work.
        val approveOnly = notificationOf(decision(rejectable = false))
        assertEquals(1, approveOnly.actions.size)
        assertTrue(approveOnly.actions[0].title.toString().contains("Approve"))

        // `actions` is null rather than empty when there are none — AndroidCompat's
        // choice, not ours: an assertion that assumed an empty array would fail on a
        // notification that is behaving exactly as intended.
        val neither = notificationOf(decision(approvable = false, rejectable = false))
        assertEquals(0, neither.actions?.size ?: 0)
    }

    @Test
    fun tappingAButtonCarriesTheRequestAndItsComputer() {
        // The action has to be self-contained: it runs in a broadcast receiver, with
        // the app possibly not running, so everything the write needs travels in the
        // intent — including which machine (a pane id alone is not an address).
        val notification = notificationOf(decision(hostId = "workbox"))
        val action = notification.actions.first { it.title.toString().contains("Approve") }
        // The intent is inside a PendingIntent, and Robolectric is how a test looks
        // at one without a running system to fire it.
        val intent = org.robolectric.Shadows.shadowOf(action.actionIntent).savedIntent

        assertNotNull(intent)
        assertEquals("wA:p1@1", intent!!.getStringExtra(PerchNotifications.EXTRA_DECISION_ID))
        assertEquals("wA:p1", intent.getStringExtra(PerchNotifications.EXTRA_PANE_ID))
        assertEquals("workbox", intent.getStringExtra(PerchNotifications.EXTRA_HOST))
    }

    @Test
    fun tappingTheBodyOpensThatSessionRatherThanJustTheApp() {
        // The body tap used to carry only the decision id, so the app opened on the
        // queue and the user had to find the session by hand. It must carry the same
        // address the action buttons do.
        val notification = notificationOf(decision(hostId = "workbox"))
        val intent = org.robolectric.Shadows.shadowOf(notification.contentIntent).savedIntent

        assertNotNull(intent)
        assertEquals("wA:p1", intent!!.getStringExtra(PerchNotifications.EXTRA_PANE_ID))
        assertEquals("workbox", intent.getStringExtra(PerchNotifications.EXTRA_HOST))
    }

    @Test
    fun aSecondRequestIsAdvertisedWithoutChangingTheFirstOne() {
        val title = notificationOf(decision(), waiting = 3).extras
            .getCharSequence(Notification.EXTRA_TITLE).toString()
        // The "+N more waiting" line is a sub-text, so the title stays about *this*
        // request: a title that becomes "3 requests" tells the user less.
        val sub = notificationOf(decision(), waiting = 3).extras
            .getCharSequence(Notification.EXTRA_SUB_TEXT)?.toString()
        assertTrue(title.contains("claude"))
        assertEquals("+2 more waiting", sub)
    }
}
