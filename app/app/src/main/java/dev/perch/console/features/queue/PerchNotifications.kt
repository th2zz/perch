package dev.perch.console.features.queue

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import dev.perch.console.MainActivity
import dev.perch.console.R
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.format.Markdown
import dev.perch.console.core.format.NotificationText

/**
 * Notification rendering.
 *
 * Two channels, because the two things the app has to say are not the same kind
 * of message:
 *
 *  - `perch_decisions` is high importance and audible. It is the whole product:
 *    an agent stopped and only a person can continue it.
 *  - `perch_watching` is silent and low importance. It is the price of staying
 *    connected to the user's own computer, and it should never make a sound.
 *
 * A note on what may appear in a notification: the *push payload* from our
 * gateway carries a summary only, never prompt text (constitution V.8). A
 * notification built here on the phone is a different thing — it is rendered
 * from data the phone fetched over its own authenticated connection to the
 * user's computer, so showing the excerpt is exactly as private as showing it in
 * the app. It is still marked VISIBILITY_PRIVATE so the lock screen respects the
 * user's "hide sensitive content" setting.
 */
object PerchNotifications {

    const val CHANNEL_DECISIONS = "perch_decisions"
    const val CHANNEL_WATCHING = "perch_watching"

    const val ACTION_APPROVE = "dev.perch.console.action.APPROVE"
    const val ACTION_REJECT = "dev.perch.console.action.REJECT"
    const val EXTRA_DECISION_ID = "decision_id"
    const val EXTRA_PANE_ID = "pane_id"
    /**
     * Which computer the decision is on.
     *
     * Carried in the intent rather than looked up when the button is tapped: the
     * tap can happen minutes later, and a pane id alone does not say which
     * machine to press the key on.
     */
    const val EXTRA_HOST = "host"

    /** The permanent notification id for the watching service. */
    const val ID_WATCHING = 1
    private const val ID_DECISION_BASE = 1000
    private const val ID_PROBLEM = 2

    /** Stable per-decision notification id, so an update replaces the old card. */
    fun idFor(decisionId: String): Int =
        ID_DECISION_BASE + (decisionId.hashCode() and 0x00FFFFFF)

    /**
     * Creates the channels. Safe to call repeatedly; the platform ignores a
     * duplicate channel definition, which is what makes this callable from both
     * the activity and the service without coordination.
     */
    fun ensureChannels(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java) ?: return

        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_DECISIONS,
                context.getString(R.string.channel_decisions),
                NotificationManager.IMPORTANCE_HIGH,
            ).apply {
                description = context.getString(R.string.channel_decisions_desc)
                // The agent's question is the point of the notification, so it is
                // allowed to be long.
                enableVibration(true)
            },
        )
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL_WATCHING,
                context.getString(R.string.channel_watching),
                NotificationManager.IMPORTANCE_MIN,
            ).apply {
                description = context.getString(R.string.channel_watching_desc)
                setShowBadge(false)
                enableVibration(false)
                setSound(null, null)
            },
        )
    }

    /**
     * True when the user has allowed notifications.
     *
     * Checked rather than assumed because a blocked notification channel would
     * otherwise look like "the product is broken" instead of "you turned this
     * off", and the difference decides what we tell the user.
     */
    fun allowed(context: Context): Boolean =
        ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED &&
            NotificationManagerCompat.from(context).areNotificationsEnabled()

    /**
     * The decision card.
     *
     * Actions are attached only when the computer said the action can actually
     * be carried out. A button that leads to a refusal is worse than no button:
     * it teaches the user that tapping does not work.
     */
    fun decision(context: Context, decision: Decision, waitingCount: Int): Notification {
        // Markers stripped: a notification cannot render Markdown, so `**bold**`
        // would reach the lock screen as punctuation. The card renders it instead
        // (see QueuePage); this is the same text, in the one form this surface has.
        val body = Markdown.plain(NotificationText.excerpt(decision.promptText))
            .ifBlank { context.getString(R.string.decision_no_detail) }

        val builder = NotificationCompat.Builder(context, CHANNEL_DECISIONS)
            .setSmallIcon(R.drawable.ic_perch_notification)
            .setContentTitle(
                if (decision.hostId.isNotBlank() && decision.hostId != Host.LOCAL) {
                    // Which computer, not only which project: two machines can have
                    // a project with the same name, and the user is being asked to
                    // act on one of them (spec FR-016).
                    context.getString(
                        R.string.decision_title_on_host,
                        decision.agentLabel,
                        decision.projectLabel,
                        decision.hostId,
                    )
                } else {
                    context.getString(R.string.decision_title, decision.agentLabel, decision.projectLabel)
                },
            )
            // Collapsed: the question. Expanded: the question and its options.
            .setContentText(Markdown.plain(NotificationText.headline(decision.promptText)).ifBlank { body })
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
            .setAutoCancel(true)
            // The watching service re-posts every still-pending card on every event
            // that moves the queue, on purpose: the shade is supposed to equal the
            // queue, so a card the user swiped away comes back while the request is
            // still open. What must not come back is the *alert*. Without this flag
            // Android treats each post as a new notification — `setWhen` is
            // re-stamped, so it even looks new — and the phone buzzes again for a
            // request the user has already been told about, every time anything else
            // happens on the computer. That reads as "something new arrived" and is
            // the fastest way to make a person stop trusting the badge.
            .setOnlyAlertOnce(true)
            .setWhen(System.currentTimeMillis())
            .setShowWhen(true)
            .setContentIntent(openApp(context, decision))

        if (waitingCount > 1) {
            builder.setSubText(context.getString(R.string.decision_others_waiting, waitingCount - 1))
        }

        if (decision.approvable) {
            builder.addAction(
                0,
                context.getString(R.string.action_approve),
                action(context, ACTION_APPROVE, decision),
            )
        }
        if (decision.rejectable) {
            builder.addAction(
                0,
                context.getString(R.string.action_reject),
                action(context, ACTION_REJECT, decision),
            )
        }
        if (!decision.approvable && !decision.rejectable) {
            // Nothing can be pressed here, so the notification has to say what to
            // do instead of offering a dead action (spec FR-015).
            builder.setSubText(context.getString(R.string.decision_needs_computer))
        }
        return builder.build()
    }

    /** The foreground service's own, deliberately quiet, notification. */
    fun watching(context: Context, text: String, live: Boolean): Notification =
        NotificationCompat.Builder(context, CHANNEL_WATCHING)
            .setSmallIcon(R.drawable.ic_perch_notification)
            .setContentTitle(context.getString(R.string.watching_title))
            .setContentText(text)
            .setPriority(NotificationCompat.PRIORITY_MIN)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setOngoing(true)
            .setSilent(true)
            .setShowWhen(false)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setContentIntent(openApp(context, null))
            .apply { if (!live) setSubText(context.getString(R.string.watching_retrying)) }
            .build()

    /**
     * Tells the user that an action did not go through, without opening the app.
     *
     * This exists because a notification action happens off-screen: if the tap
     * silently failed, the user would believe the agent had been unblocked.
     */
    fun problem(context: Context, message: String, fix: String) {
        val notification = NotificationCompat.Builder(context, CHANNEL_DECISIONS)
            .setSmallIcon(R.drawable.ic_perch_notification)
            .setContentTitle(context.getString(R.string.problem_title))
            .setContentText(message)
            .setStyle(NotificationCompat.BigTextStyle().bigText("$message\n$fix"))
            .setPriority(NotificationCompat.PRIORITY_DEFAULT)
            .setAutoCancel(true)
            .setContentIntent(openApp(context, null))
            .build()
        notify(context, ID_PROBLEM, notification)
    }

    fun cancel(context: Context, decisionId: String) {
        NotificationManagerCompat.from(context).cancel(idFor(decisionId))
    }

    /**
     * Removes every notification this app has posted.
     *
     * Called when the user unpairs, and it is the notification half of the same
     * rule as `AppContainer.forgetPairing`: unpairing means forgetting what we
     * showed about the computer, not only what we stored. The cards in the shade
     * are all about a machine we no longer know. Left alone they would (a) invite
     * a tap whose answer is "you are not paired" — a refusal the user did nothing
     * to earn — and (b) on the next pairing sit next to the new computer's cards
     * looking as if they belonged to it.
     *
     * `cancelAll` rather than a loop over ids on purpose: a decision notification's
     * id is derived from the decision id, and the set of ids that ever existed is
     * not something the app keeps. "Everything this app posted" is precisely the
     * set we want gone, and it is the only version of that set that cannot be
     * incomplete.
     */
    fun cancelAll(context: Context) {
        NotificationManagerCompat.from(context).cancelAll()
    }

    fun notify(context: Context, id: Int, notification: Notification) {
        // The permission check is written out here rather than delegated to
        // `allowed` for two reasons. Lint can only see a check it can read, and
        // without one this is a crash-class error rather than a warning. And the
        // platform genuinely revokes this permission: a user who turns
        // notifications off must get nothing, not a SecurityException from a
        // background service that then looks like a bug in the app.
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
        ) {
            return
        }
        val manager = NotificationManagerCompat.from(context)
        if (!manager.areNotificationsEnabled()) return
        runCatching { manager.notify(id, notification) }
    }

    // ------------------------------------------------------------ internals

    private fun action(
        context: Context,
        actionName: String,
        decision: Decision,
    ): PendingIntent {
        val intent = Intent(context, DecisionActionReceiver::class.java)
            .setAction(actionName)
            .putExtra(EXTRA_DECISION_ID, decision.id)
            .putExtra(EXTRA_PANE_ID, decision.paneId)
            .putExtra(EXTRA_HOST, decision.hostId)
        return PendingIntent.getBroadcast(
            context,
            // Distinct per decision and per action, otherwise Android would hand
            // the user the first-built PendingIntent with the wrong decision id.
            (decision.id + actionName).hashCode(),
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }

    private fun openApp(context: Context, decision: Decision?): PendingIntent {
        val intent = Intent(context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
            .putExtra(EXTRA_DECISION_ID, decision?.id)
        // Tapping the body of a request notification is a request to see the
        // session, not merely to open the app. The action buttons already carry
        // these two extras; the body did not, which is why it used to land on the
        // queue and leave the user to find the session by hand.
        decision?.paneId?.let { intent.putExtra(EXTRA_PANE_ID, it) }
        decision?.hostId?.let { intent.putExtra(EXTRA_HOST, it) }
        return PendingIntent.getActivity(
            context,
            decision?.id?.hashCode() ?: 0,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
    }
}
