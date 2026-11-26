package dev.perch.console.core.startup

/**
 * How long the user waited, from tapping the icon to seeing the queue.
 *
 * The first version of this measurement reported `firstFrame - processStart`, and
 * the trial found out why that is wrong (2026-10-13, first real phone): the app's
 * process is *not* started by the tap. The watching service is a foreground
 * service in the same process, and it is started at boot — so by the time the user
 * opens the app the process may have been running for minutes, and the number
 * reported "how long after the process started you got round to tapping". It read
 * **9,874 ms** against a target of two seconds, on a phone where the app in fact
 * appeared immediately. A measurement that blames the product for the user's own
 * delay is worse than no measurement: it sends someone to optimise a startup path
 * that is already fast.
 *
 * So the origin depends on which case this is, and the two cases are told apart by
 * whether the process predates the launch:
 *
 *  * **The process was already running** (the ordinary case — the service keeps it
 *    alive): the wait began when the launch did, so measure from there.
 *  * **The process started because of this launch** (a genuine cold start: after a
 *    reboot, or after the app was stopped): the wait includes creating the process,
 *    so measure from the process start.
 *
 * The choice is a pure function so it can be tested without a phone, which matters
 * because the phone is exactly where it went wrong.
 */
data class StartupMeasurement(
    val millis: Long,
    val from: Origin,
) {
    enum class Origin {
        /** The user's tap started the process: this is a real cold start. */
        Process,

        /** The process was already alive; the wait is this launch's own cost. */
        Launch,
    }

    companion object {
        /**
         * How soon after the launch a process start still counts as "this launch
         * started it".
         *
         * Not zero, because the two clocks are taken at different points (the
         * process begins, then the activity's onCreate runs) and a cold start costs
         * a few hundred milliseconds of framework work that belongs in the number.
         * Not large either: a second or two is the whole range of legitimate
         * process-start work, while "the service started it at boot" is minutes
         * away from any plausible launch.
         */
        const val COLD_PROCESS_WINDOW_MILLIS = 2_000L

        /**
         * Measures the wait.
         *
         * [launchStartMillis] may be 0 when it was never recorded — a launch that
         * did not come through the activity, or a code path that forgot to mark it.
         * Then the process start is the only origin there is, and it is used with
         * the caveat that the number may include time the user spent elsewhere.
         */
        fun measure(
            processStartMillis: Long,
            launchStartMillis: Long,
            firstFrameMillis: Long,
        ): StartupMeasurement {
            val known = launchStartMillis > 0
            val processPredatesLaunch =
                known && launchStartMillis - processStartMillis > COLD_PROCESS_WINDOW_MILLIS
            return if (known && processPredatesLaunch) {
                StartupMeasurement(
                    millis = (firstFrameMillis - launchStartMillis).coerceAtLeast(0),
                    from = Origin.Launch,
                )
            } else {
                StartupMeasurement(
                    millis = (firstFrameMillis - processStartMillis).coerceAtLeast(0),
                    from = Origin.Process,
                )
            }
        }
    }
}
