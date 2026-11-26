package dev.perch.console

import dev.perch.console.core.herdrclient.ManageLabels
import dev.perch.console.features.manage.ManageCopy
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The words that stand between a tap and somebody's work.
 *
 * Two of the three actions this feature adds cannot be undone, so what the dialogs *say* is the feature: that
 * an agent is working right now, that this is the last session in a project and closing it takes the project
 * too, and that a name too long for the list will not be sent anywhere. A dialog whose sentences are wrong is
 * worse than no dialog — it teaches people to tap through the next one.
 */
class ManageDialogsTest {

    private val label = ManageLabels

    // ------------------------------------------------------------------ the rules the fields obey

    @Test
    fun aNameIsCheckedBeforeAnythingIsSent() {
        assertNull(label.problem("fix the parser"))
        assertNull(label.problem("  backend  api  "))
        // Trimmed, then judged: two spaces are not a name.
        assertEquals("A name cannot be empty.", label.problem("   "))
        assertEquals("A name cannot be empty.", label.problem(""))
        // Bounded because a list row on a phone is bounded.
        assertEquals("A name can be at most 80 characters.", label.problem("x".repeat(label.MAX + 1)))
        assertNull(label.problem("x".repeat(label.MAX)))
        // A newline in a label is a line the terminal would draw that nobody wrote.
        assertEquals(
            "A name cannot contain line breaks or tabs.",
            label.problem("two\nlines"),
        )
        assertEquals(
            "A name cannot contain line breaks or tabs.",
            label.problem("tab\there"),
        )
    }

    // ------------------------------------------------------------------ what a confirmation must say
    //
    // Tested as a decision rather than by reading it off a rendered dialog. That is not a shortcut around the
    // screen: rendering these dialogs in this harness exhausted the test JVM (OutOfMemoryError inside
    // Robolectric's text layout, on a test with no text field in it), and a check that dies of memory pressure
    // proves nothing about the sentences. What matters is that every combination of facts produces the right
    // warnings, and that is exactly what this holds still — the screen's only job is to draw them.

    @Test
    fun anIdleSessionInABusyProjectGetsNoWarning() {
        assertEquals(
            emptyList<ManageCopy.Warning>(),
            ManageCopy.closeSessionWarnings(working = false, lastInProject = false),
        )
    }

    @Test
    fun aWorkingAgentIsAWarning() {
        assertEquals(
            listOf(ManageCopy.Warning.AgentIsWorking),
            ManageCopy.closeSessionWarnings(working = true, lastInProject = false),
        )
    }

    @Test
    fun theLastSessionInAProjectIsAWarningEvenWhenNothingIsWorking() {
        // The surprise this exists for: an idle agent, a tidy-looking close, and a project that goes with it.
        assertEquals(
            listOf(ManageCopy.Warning.LastSessionInProject),
            ManageCopy.closeSessionWarnings(working = false, lastInProject = true),
        )
    }

    @Test
    fun bothWarningsAppearWhenBothAreTrueWithTheImmediateOneFirst() {
        val warnings = ManageCopy.closeSessionWarnings(working = true, lastInProject = true)
        // Order is part of the design: what is happening in *this* session comes before the project surprise,
        // because a phone dialog that leads with the unexpected buries the immediate.
        assertEquals(
            listOf(ManageCopy.Warning.AgentIsWorking, ManageCopy.Warning.LastSessionInProject),
            warnings,
        )
    }

    @Test
    fun aProjectWithNothingInItIsNotDescribedAsIfItHadSessions() {
        assertFalse(ManageCopy.projectCloseNamesCount(0))
        assertTrue(ManageCopy.projectCloseNamesCount(1))
        assertTrue(ManageCopy.projectCloseNamesCount(4))
    }
}
