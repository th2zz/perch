package dev.perch.console.features.manage

/**
 * Which warnings a confirmation has to carry, decided without drawing anything.
 *
 * This exists because of a test that could not be written the obvious way. Rendering the dialog and reading
 * the sentence off it is the direct check, and in this project's test harness it is also the one that runs out
 * of memory: a Robolectric text field and a dialog per case is enough to exhaust the test JVM, and a test that
 * dies of `OutOfMemoryError` says nothing about whether the warning is right.
 *
 * So the decision is separated from the drawing. This function is the whole of "what must this dialog say",
 * it is cheap enough to test in every combination, and the screen job is only to put the sentences on it. When
 * a rule about warnings changes, it changes here, and the combinations are already written down.
 */
object ManageCopy {

    /**
     * A fact worth interrupting somebody for.
     *
     * These are the two things that decide whether a tap is tidying up or destroying work: an agent that is
     * mid-task, and a project that goes with the session because it holds nothing else.
     */
    enum class Warning {
        /** The agent in this session is working right now, and closing it stops that work. */
        AgentIsWorking,

        /**
         * This is the only session in its project, so closing it closes the project too.
         *
         * Measured against a live herdr on 2026-10-15: `pane close` on the last pane of a workspace takes the
         * workspace with it — herdr has no such thing as an empty workspace. Nobody expects that, so it is
         * said out loud before the tap.
         */
        LastSessionInProject,
    }

    /**
     * What the close-session dialog must say, in the order a person should read it.
     *
     * The project warning comes second because the working one is about *this* session, and a phone screen that
     * leads with the surprising warning buries the immediate one.
     */
    fun closeSessionWarnings(working: Boolean, lastInProject: Boolean): List<Warning> = buildList {
        if (working) add(Warning.AgentIsWorking)
        if (lastInProject) add(Warning.LastSessionInProject)
    }

    /**
     * Whether a close-project confirmation has to count what is running in it.
     *
     * Zero sessions is a project that holds nothing — "every session in this project stops" is then a true but
     * empty sentence, and counting to zero out loud reads like a bug.
     */
    fun projectCloseNamesCount(sessionCount: Int): Boolean = sessionCount > 0
}
