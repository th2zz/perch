package dev.perch.console

import android.Manifest
import android.app.NotificationManager
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.cache.CachedState
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.outbox.QueuedReply
import dev.perch.console.features.console.ConsoleViewModel
import dev.perch.console.features.queue.PerchNotifications
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class UnpairForgetsTheComputerTest {

    private val app: PerchApp = ApplicationProvider.getApplicationContext()
    private val container = app.container

    private fun manager() = app.getSystemService(NotificationManager::class.java)!!

    private fun postedCount() = shadowOf(manager()).size()

    private fun pendingReply() = QueuedReply(
        decisionId = "d1",
        paneId = "wA:p1",
        kind = "answer",
        text = "yes, go ahead",
        queuedAtMillis = 1_760_000_000_000,
    )

    private fun card() = Decision(
        id = "d1",
        hostId = "local",
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agentKind = "claude",
        displayAgent = "claude",
        promptText = "Do you want to proceed?",
        approvable = true,
        rejectable = true,
    )

    @Test
    fun nothingAboutTheOldComputerOutlivesAnUnpair() {
        // Granting the permission is load-bearing: without it `PerchNotifications.notify`
        // returns early by design, and the notification half of this test would pass
        // against a phone that never shows anything — a green that means nothing.
        shadowOf(app).grantPermissions(Manifest.permission.POST_NOTIFICATIONS)

        // The ordinary sequence: the laptop was asleep, so "yes" went to the outbox,
        // its card is in the shade, and the last known state is in the mirror cache.
        container.outbox.enqueue(pendingReply())
        container.cacheState(CachedState(savedAtMillis = 1_760_000_000_000))
        PerchNotifications.notify(
            app,
            PerchNotifications.idFor("d1"),
            PerchNotifications.decision(app, card(), waitingCount = 1),
        )
        assertEquals("the test needs a card in the shade to prove it is cleared", 1, postedCount())

        // Before the connection came back, the user unpaired and paired elsewhere.
        ConsoleViewModel(app).unpair()

        assertEquals(
            "an unpair must clear the pending replies: a reply written for one " +
                "computer must never reach the next",
            0,
            container.outbox.size(),
        )
        assertEquals(
            "an unpair must clear the cards: they answer this computer's agents and " +
                "must not sit next to the next computer's looking like they belong to it",
            0,
            postedCount(),
        )
        // The whole pairing is forgotten, not just the token: the mirror cache goes
        // too, so nothing about the old computer is left to be shown or resent.
        assertNull(container.cachedState())
        assertNull(container.pairingStore.load())
    }
}
