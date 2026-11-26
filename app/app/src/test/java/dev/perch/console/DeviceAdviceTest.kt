package dev.perch.console

import dev.perch.console.core.settings.DeviceAdvice
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Which warnings this app is allowed to show, and when.
 *
 * The failure this guards against is not a missing warning but a *cheapened* one: a
 * screen that shows every warning it has, every time, is a screen nobody reads, and
 * the two warnings here are the ones that matter most (spec FR-015 requires a reason
 * and a next step; the whole product depends on the phone being reachable).
 *
 * The rules are pure, so they are checked here rather than through a device.
 */
class DeviceAdviceTest {

    private fun advice(
        paired: Boolean = true,
        watching: Boolean = true,
        notificationsBlocked: Boolean = false,
        backgroundRestricted: Boolean = false,
    ) = DeviceAdvice.forFacts(paired, watching, notificationsBlocked, backgroundRestricted)

    @Test
    fun aHealthilyconfiguredPhoneIsToldNothing() {
        assertTrue(advice().isEmpty())
    }

    @Test
    fun notificationsOffIsAlwaysWorthSaying() {
        val items = advice(notificationsBlocked = true)
        assertEquals(1, items.size)
        assertEquals(R.string.notifications_blocked, items[0].message)
        assertEquals(DeviceAdvice.Fix.NotificationSettings, items[0].fix)
        // Still said when the user has not asked for background watching: the app
        // cannot notify even while it is open.
        assertEquals(1, advice(watching = false, notificationsBlocked = true).size)
    }

    @Test
    fun aRestrictedPhoneIsOnlyWarnedWhenTheUserAskedForBackgroundWatching() {
        // With the toggle off, the promise is "only while the app is open", and the
        // restriction does not apply to that promise — warning about it would be a
        // false alarm on a phone that is behaving exactly as promised.
        assertTrue(advice(watching = false, backgroundRestricted = true).isEmpty())

        val items = advice(backgroundRestricted = true)
        assertEquals(1, items.size)
        assertEquals(R.string.battery_restricted, items[0].message)
        assertEquals(DeviceAdvice.Fix.BatterySettings, items[0].fix)
    }

    @Test
    fun bothFailuresAreShownWithTheWorseOneFirst() {
        val items = advice(notificationsBlocked = true, backgroundRestricted = true)
        assertEquals(2, items.size)
        // A phone that cannot notify at all is worse than one that may stop later.
        assertEquals(R.string.notifications_blocked, items[0].message)
        assertEquals(R.string.battery_restricted, items[1].message)
    }

    @Test
    fun nothingIsClaimedBeforeThereIsAComputerToHearFrom() {
        // Advice about a product that is not set up yet is noise, and it is how a
        // first screen ends up full of warnings nobody caused.
        assertTrue(
            advice(paired = false, notificationsBlocked = true, backgroundRestricted = true).isEmpty(),
        )
    }
}
