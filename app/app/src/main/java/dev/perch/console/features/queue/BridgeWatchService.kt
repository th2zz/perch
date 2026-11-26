package dev.perch.console.features.queue

import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.IBinder
import android.os.SystemClock
import androidx.core.content.ContextCompat
import dev.perch.console.PerchApp
import dev.perch.console.R
import dev.perch.console.core.herdrclient.WireValues
import dev.perch.console.core.live.BridgeEventTypes
import dev.perch.console.core.live.BridgeStream
import dev.perch.console.core.live.ConnectionStatus
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

/**
 * Keeps the phone reachable while the user is away from the desk.
 *
 * Why a foreground service rather than a push-only design: the production push
 * path (FCM) requires a Google service account and a Firebase project, and the
 * product must work end to end before that exists. A persistent connection to
 * the user's *own* machine needs no third party at all — the phone talks to the
 * computer it already trusts, over a network the user already has.
 *
 * This is also, not coincidentally, how Tailscale does it: a small ongoing
 * notification for something that must stay connected. The notification is
 * deliberately the quietest the platform offers, on its own channel, so it can
 * be silenced without silencing the decisions that matter.
 *
 * The trade is honest and stated: the app holds a socket open, which costs some
 * battery. In exchange, a decision reaches the phone in about a second rather
 * than within the push system's best effort, and nothing about the user's code
 * passes through anybody else's servers.
 */
class BridgeWatchService : Service() {

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)
    private var everNotified = mutableSetOf<String>()
    /**
     * When the phone heard about a decision, on the only clock that can measure a
     * duration on a single device.
     */
    private var lastEventAt = 0L

    /**
     * One sync at a time, with at most one more queued.
     *
     * The service is the one that runs while the app is closed, so it is the one
     * whose waste shows up as battery. Agents change state in bursts, and each
     * event used to buy its own full read of the session.
     */
    private var syncing = false
    private var syncAgain = false

    /** True once the status and event collectors are installed for this instance. */
    private var wired = false

    override fun onCreate() {
        super.onCreate()
        PerchNotifications.ensureChannels(this)
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // The platform requires a foreground notification within a few seconds of
        // the start, so it goes up before anything else is attempted.
        startForeground(
            PerchNotifications.ID_WATCHING,
            PerchNotifications.watching(this, getString(R.string.watching_connecting), live = false),
        )

        val container = (application as PerchApp).container
        BridgeStream.start { container.client() }

        // One service, one pair of collectors. This matters because the app now
        // *ensures* the service is running when it is opened (see
        // ConsoleViewModel.ensureWatching): before this guard, every one of those
        // calls would have stacked another collector on the same connection, so the
        // work per event would grow with the number of times the user opened the app.
        if (wired) {
            // Still a good moment to ask what is waiting: one request, no new
            // collectors.
            scope.launch { syncNotifications() }
            return START_STICKY
        }
        wired = true

        scope.launch {
            BridgeStream.status.collectLatest { status ->
                updateWatchingNotification(status, container.client() != null)
            }
        }

        scope.launch {
            BridgeStream.events.collect {
                // Stamped before any work, so the measurement covers the whole of
                // this phone's own latency rather than the part after bookkeeping.
                lastEventAt = SystemClock.elapsedRealtime()
                // The names and the set live in BridgeEventTypes, shared with the
                // bridge through a contract file; the heartbeat is deliberately not
                // one of them, and nothing else is ignored silently.
                if (BridgeEventTypes.movesTheQueue(it.type)) {
                    // Any of these means the queue may have changed. The service
                    // re-reads rather than trusting the event payload, for the same
                    // reason the console does: the queue must mirror the computer.
                    syncNotifications()
                }
            }
        }

        // Re-read once on start: notifications must appear for requests that were
        // already waiting before the service came up (for example after a reboot).
        scope.launch { syncNotifications() }

        return START_STICKY
    }

    /**
     * Makes the set of notifications equal the computer's pending queue.
     *
     * Equality in both directions matters: requests that appeared while the phone
     * was asleep must show up, and requests the user already answered on the
     * computer must disappear. A notification that outlives its request is a
     * request to do something that is already done.
     */
    private suspend fun syncNotifications() {
        if (syncing) {
            syncAgain = true
            return
        }
        syncing = true
        try {
            syncNotificationsOnce()
        } finally {
            syncing = false
            if (syncAgain) {
                syncAgain = false
                // Whatever arrived while this read was in flight wanted a newer
                // answer, so it gets exactly one more read.
                syncNotificationsOnce()
            }
        }
    }

    private suspend fun syncNotificationsOnce() {
        val container = (application as PerchApp).container
        val client = container.client() ?: return
        val decisions = try {
            client.snapshot().decisions
        } catch (_: Exception) {
            // Offline: keep what is on screen. The cached queue is a better answer
            // than an empty notification tray.
            return
        }

        if (!PerchNotifications.allowed(this)) return

        val pending = decisions.filter { it.state == WireValues.DecisionState.PENDING }
        pending.forEach { decision ->
            val firstTime = decision.id !in everNotified
            PerchNotifications.notify(
                this,
                PerchNotifications.idFor(decision.id),
                PerchNotifications.decision(this, decision, pending.size),
            )
            if (firstTime && lastEventAt > 0) {
                reportShown(client, decision.id)
            }
            everNotified.add(decision.id)
        }

        val stillPending = pending.map { it.id }.toSet()
        (everNotified - stillPending).forEach { gone ->
            PerchNotifications.cancel(this, gone)
            everNotified.remove(gone)
        }
    }

    /**
     * Tells the computer how long this phone took.
     *
     * Fire and forget: the number is worth having, and it is not worth delaying a
     * notification, a reconnection, or anything else the user can feel.
     */
    private fun reportShown(client: dev.perch.console.core.herdrclient.BridgeClient, decisionID: String) {
        val elapsed = (SystemClock.elapsedRealtime() - lastEventAt).coerceAtLeast(0)
        scope.launch { client.reportNotificationShown(decisionID, elapsed) }
    }

    private fun updateWatchingNotification(status: ConnectionStatus, paired: Boolean) {
        val text = when {
            !paired -> getString(R.string.watching_unpaired)
            status == ConnectionStatus.Live -> getString(R.string.watching_live)
            else -> getString(R.string.watching_connecting)
        }
        PerchNotifications.notify(
            this,
            PerchNotifications.ID_WATCHING,
            PerchNotifications.watching(this, text, live = status == ConnectionStatus.Live),
        )
    }

    override fun onDestroy() {
        scope.cancel()
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    companion object {
        /**
         * Starts watching, from anywhere.
         *
         * `startForegroundService` is used on every supported version; the caller
         * is always in a position where starting a foreground service is allowed
         * (the user just paired, or the device just booted), which is why this is
         * safe to call directly rather than only from the activity.
         */
        fun start(context: Context) {
            ContextCompat.startForegroundService(
                context,
                Intent(context, BridgeWatchService::class.java),
            )
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, BridgeWatchService::class.java))
        }
    }
}
