package dev.perch.console

import dev.perch.console.core.startup.StartupMeasurement
import dev.perch.console.core.startup.StartupMeasurement.Companion.measure
import dev.perch.console.core.startup.StartupMeasurement.Origin
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The measurement that blamed the app for the user's own delay.
 *
 * On the first real trial (2026-10-13) the report said the cold start took
 * **9,874 ms** against a target of two seconds. Nothing was slow: the watching
 * service keeps the app's process alive from boot, so "time since the process
 * started" was measuring how long the user took to switch back from another app
 * and tap the icon. Reconstructing that exact case is the first test here, because
 * a number that sends you to optimise a fast path is worse than no number.
 *
 * These tests are on plain numbers, so they say what the policy is without a
 * device: which origin is chosen, and what is reported.
 */
class StartupMeasurementTest {

    @Test
    fun theTrialCaseReportsWhatTheUserWaitedAndNotWhatTheProcessHasBeenAliveFor() {
        // Reconstructed from latency.json and the bridge's request log:
        //   process start ≈ 17:03:46 (the service was started at boot)
        //   launch        ≈ 17:03:56 (the user tapped, ten seconds later)
        //   first frame   ≈ 17:03:56.2 (the app appeared at once)
        val measurement = measure(
            processStartMillis = 0,
            launchStartMillis = 9_874,
            firstFrameMillis = 10_074,
        )

        assertEquals("the app itself took 200ms, which is the truth", 200, measurement.millis)
        assertEquals(Origin.Launch, measurement.from)
    }

    @Test
    fun aGenuineColdStartStillCountsTheProcessBeingCreated() {
        // After a reboot (or after the app was stopped) the process is created by
        // this launch, and creating it is work the user waits for. Measuring from
        // the launch alone would hide it.
        val measurement = measure(
            processStartMillis = 0,
            launchStartMillis = 300,
            firstFrameMillis = 1_500,
        )

        assertEquals("a cold start is measured from the beginning", 1_500, measurement.millis)
        assertEquals(Origin.Process, measurement.from)
    }

    @Test
    fun anUnrecordedLaunchFallsBackToTheProcessRatherThanGuessing() {
        // 0 means "nothing recorded it" (a launch that did not come through the
        // activity). The process start is then the only origin there is; claiming a
        // small number from a launch we never saw would be an invention.
        val measurement = measure(
            processStartMillis = 1_000,
            launchStartMillis = 0,
            firstFrameMillis = 1_800,
        )

        assertEquals(800, measurement.millis)
        assertEquals(Origin.Process, measurement.from)
    }

    @Test
    fun theBoundaryIsWhereProcessWorkStopsBeingProcessWork() {
        val window = StartupMeasurement.COLD_PROCESS_WINDOW_MILLIS

        // Just inside: the process was created as part of this launch.
        assertEquals(
            Origin.Process,
            measure(processStartMillis = 0, launchStartMillis = window, firstFrameMillis = window + 100).from,
        )
        // Just outside: the process predates the launch, so it was already running.
        assertEquals(
            Origin.Launch,
            measure(processStartMillis = 0, launchStartMillis = window + 1, firstFrameMillis = window + 101).from,
        )
    }

    @Test
    fun clockOdditiesCannotProduceANegativeDuration() {
        // Both clocks are uptime, so this should not happen; if it ever does, a
        // negative "it took -3 seconds" is not something to show a user.
        val measurement = measure(processStartMillis = 5_000, launchStartMillis = 0, firstFrameMillis = 4_000)
        assertEquals(0, measurement.millis)
    }
}
