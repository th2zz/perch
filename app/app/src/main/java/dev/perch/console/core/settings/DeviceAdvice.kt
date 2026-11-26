package dev.perch.console.core.settings

import androidx.annotation.StringRes
import dev.perch.console.R

/**
 * The two settings that make this product silently stop working.
 *
 * Both have the same shape, and it is the shape that makes them dangerous: nothing
 * in the app looks different. The agent waits, the phone says nothing, and the user
 * concludes the product is broken rather than that Android is doing what it was told.
 *
 *   1. **Notifications are off for Perch.** The phone is allowed to run the watch
 *      service and is simply forbidden to show anything.
 *   2. **The phone restricts background apps.** Some manufacturers (Xiaomi, Huawei,
 *      Oppo, and others) stop a background service after a while even though Android
 *      allows it — and the *only* sign is that notifications stop arriving some
 *      minutes after the screen goes off.
 *
 * This is a pure function of what the phone reports, so the rules can be tested
 * without a device: given these facts, which sentences does the user need to see?
 */
object DeviceAdvice {

    /** Which settings screen the fix opens. */
    enum class Fix {
        /** This app's notification settings. */
        NotificationSettings,

        /** The list where background restrictions are lifted per app. */
        BatterySettings,
    }

    data class Item(
        @param:StringRes val message: Int,
        @param:StringRes val action: Int,
        val fix: Fix,
    )

    /**
     * What the user needs to be told, given what the phone reports.
     *
     * The order is by severity: a phone that cannot notify at all is worse than one
     * that may stop watching later.
     */
    fun forFacts(
        paired: Boolean,
        watchingInBackground: Boolean,
        notificationsBlocked: Boolean,
        backgroundRestricted: Boolean,
    ): List<Item> {
        // Nothing is claimed before there is a computer to hear from: advice about
        // a product that is not set up yet is noise, and it is how a first screen
        // ends up full of warnings.
        if (!paired) return emptyList()

        val items = mutableListOf<Item>()
        if (notificationsBlocked) {
            items += Item(
                message = R.string.notifications_blocked,
                action = R.string.notifications_blocked_fix,
                fix = Fix.NotificationSettings,
            )
        }
        if (watchingInBackground && backgroundRestricted) {
            // Only when the user asked for background watching: with the toggle off
            // the promise is already "only while the app is open", and telling them
            // about a restriction that does not apply yet would be a false alarm.
            items += Item(
                message = R.string.battery_restricted,
                action = R.string.battery_restricted_fix,
                fix = Fix.BatterySettings,
            )
        }
        return items
    }
}
