package dev.perch.console

import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.herdrclient.Pane
import dev.perch.console.core.herdrclient.Workspace
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Telling two computers apart, in the app (spec FR-016).
 *
 * These are the decisions that make the difference between "one view, clearly
 * labelled" and a screen that quietly mixes two machines: which project a pane
 * belongs to, whether the computer's name is worth showing at all, and what to
 * call a record that arrived without one.
 *
 * The ids are deliberately identical across the two hosts here, because that is
 * not an edge case — every herdr machine numbers its first workspace `w1` and its
 * first pane `w1:p1`.
 */
class HostLabelingTest {

    private fun twoMachines() = Mirror(
        hosts = listOf(
            Host(hostId = "local", label = "dev-laptop", kind = "local", status = "reachable"),
            Host(hostId = "workbox", label = "workbox", kind = "ssh", status = "reachable"),
        ),
        workspaces = listOf(
            Workspace(workspaceId = "w1", label = "perch", hostId = "local"),
            Workspace(workspaceId = "w1", label = "api", hostId = "workbox"),
        ),
        panes = listOf(
            Pane(paneId = "w1:p1", workspaceId = "w1", hostId = "local", agent = "codex"),
            Pane(paneId = "w1:p1", workspaceId = "w1", hostId = "workbox", agent = "claude"),
        ),
    )

    @Test
    fun theSameWorkspaceIdOnTwoComputersIsTwoProjects() {
        assertEquals("perch", twoMachines().workspace("local", "w1")?.label)
        assertEquals("api", twoMachines().workspace("workbox", "w1")?.label)
    }

    @Test
    fun theSamePaneIdOnTwoComputersIsTwoPanes() {
        val mirror = twoMachines()
        assertEquals("codex", mirror.panesOf("local", "w1").single().agent)
        assertEquals("claude", mirror.panesOf("workbox", "w1").single().agent)
        // One workspace's panes must never include the other machine's, or the
        // project screen would show a session that is not on that computer.
        assertEquals(1, mirror.panesOf("workbox", "w1").size)
    }

    @Test
    fun aComputerIsNamedOnlyWhenThereIsMoreThanOne() {
        // With one machine the name is noise; with two, every list has to answer
        // "which computer is this on?".
        assertTrue(twoMachines().showHostNames)
        val single = twoMachines().copy(
            hosts = listOf(Host(hostId = "local", label = "dev-laptop", status = "reachable")),
            workspaces = listOf(Workspace(workspaceId = "w1", label = "perch", hostId = "local")),
        )
        assertFalse(single.showHostNames)
    }

    @Test
    fun aComputerThatDidNotAnswerStillAppears() {
        val mirror = twoMachines().copy(
            hosts = listOf(
                Host(hostId = "local", label = "dev-laptop", status = "reachable"),
                Host(
                    hostId = "workbox",
                    label = "workbox",
                    status = "unreachable",
                    reason = "Perch could not reach workbox.",
                    fix = "Check that the computer is on and reachable.",
                ),
            ),
        )
        // It is still listed (so its projects do not look like projects that
        // vanished) but it no longer counts as a second working machine.
        assertEquals(2, mirror.hosts.size)
        assertFalse(mirror.showHostNames)
    }

    @Test
    fun aRecordWithNoComputerBelongsToThisOne() {
        // A mirror written before multi-host existed has no host fields at all, and
        // the first version of the grouped project list dropped exactly those rows:
        // the rendered screen simply had no projects on it.
        val old = Mirror(
            hosts = listOf(Host(hostId = "local", label = "dev-laptop")),
            workspaces = listOf(Workspace(workspaceId = "w2", label = "backend")),
            panes = listOf(Pane(paneId = "w2:p1", workspaceId = "w2", agent = "codex")),
        )
        assertEquals("backend", old.workspace("local", "w2")?.label)
        assertEquals("backend", old.workspace("", "w2")?.label)
        assertEquals(1, old.panesOf("local", "w2").size)
        assertEquals(listOf("local"), old.hostIdsWithContent())
        assertEquals("dev-laptop", old.hostLabel(""))
    }

    @Test
    fun aRequestCarriesTheComputerItCameFrom() {
        val decision = Decision(id = "workbox/w1:p1@1", hostId = "workbox", paneId = "w1:p1")
        // The card, the notification and the write all read this one field; the
        // test exists so that dropping it is a red test rather than a keystroke
        // on the wrong computer.
        assertEquals("workbox", decision.hostId)
        assertFalse(decision.hostId == Host.LOCAL)
    }
}
