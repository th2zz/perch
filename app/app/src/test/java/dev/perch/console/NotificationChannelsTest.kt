package dev.perch.console

import android.app.Application
import android.app.NotificationManager
import androidx.test.core.app.ApplicationProvider
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
 * The two channels, and why each one is set up the way it is.
 *
 * Android decides what a notification *does* — whether it makes a sound, whether it
 * appears over the lock screen — from its channel, not from the notification. Both
 * settings here are load-bearing and both fail silently:
 *
 *  - The decision channel must be high importance, or the product's whole promise
 *    ("your phone calls you back") becomes a line in the shade nobody sees.
 *  - The watching channel must stay quiet, or the price of staying connected
 *    becomes a phone that beeps on its own every time it reconnects.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class NotificationChannelsTest {

    private val context: Application = ApplicationProvider.getApplicationContext()

    private fun manager() = context.getSystemService(NotificationManager::class.java)!!

    private fun channels(): List<android.app.NotificationChannel> {
        PerchNotifications.ensureChannels(context)
        return manager().notificationChannels
    }

    private fun channel(id: String) = channels().firstOrNull { it.id == id }

    @Test
    fun theDecisionChannelIsLoudEnoughToBeHeard() {
        val decisions = channel(PerchNotifications.CHANNEL_DECISIONS)
        assertNotNull("the decision channel is missing", decisions)
        assertEquals(
            "a decision must be able to make a sound and appear over the lock screen",
            NotificationManager.IMPORTANCE_HIGH,
            decisions!!.importance,
        )
    }

    @Test
    fun theWatchingChannelNeverMakesANoise() {
        val watching = channel(PerchNotifications.CHANNEL_WATCHING)
        assertNotNull("the watching channel is missing", watching)
        // LOW: shows in the shade, no sound. The permanent notification is the price
        // of holding a connection, and a price that beeps is a bug.
        assertTrue(
            "the watching channel is too loud: importance ${watching!!.importance}",
            watching.importance <= NotificationManager.IMPORTANCE_LOW,
        )
        assertFalse(
            "the watching channel has a sound set",
            runCatching { watching.sound != null }.getOrDefault(false),
        )
    }

    @Test
    fun bothChannelsAreExplainedInTheUsersOwnTerms() {
        for (id in listOf(PerchNotifications.CHANNEL_DECISIONS, PerchNotifications.CHANNEL_WATCHING)) {
            val channel = channel(id) ?: error("$id is missing")
            assertTrue("$id has no name the user can read", channel.name.isNotBlank())
            assertTrue("$id has no description", !channel.description.isNullOrBlank())
        }
    }

    @Test
    fun oneRequestKeepsOneNotificationId() {
        // The id is derived from the decision, so a second notification for the same
        // request *replaces* the first instead of stacking: two cards for one request
        // would look like two requests.
        val id = PerchNotifications.idFor("workbox/wA:p1@1")
        assertEquals(id, PerchNotifications.idFor("workbox/wA:p1@1"))
        assertTrue(
            "different requests must not collide",
            id != PerchNotifications.idFor("workbox/wA:p1@2"),
        )
        assertTrue(
            "the watching notification must not share an id with a decision",
            id != PerchNotifications.ID_WATCHING,
        )
    }
}
