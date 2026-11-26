package dev.perch.console

import dev.perch.console.features.manage.CreateRules
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class CreateRulesTest {

    @Test
    fun eachLineIsOneArgument() {
        assertEquals(
            listOf("--model", "gpt-5", "--sandbox", "workspace-write"),
            CreateRules.args("--model\ngpt-5\n\n--sandbox\nworkspace-write\n"),
        )
    }

    @Test
    fun spacesInsideAnArgumentAreKept() {
        assertEquals(listOf("two words"), CreateRules.args("two words"))
    }

    @Test
    fun aProjectCannotBeCreatedWithoutAWorkingFolder() {
        // This is the #148 rule: everything else can be ready and the button still stays off.
        assertFalse(CreateRules.canCreateProject("demo", "claude", "", false, false, true))
        assertFalse(CreateRules.canCreateProject("demo", "claude", "   ", false, false, true))
        assertTrue(CreateRules.canCreateProject("demo", "claude", "/home/me/work", false, false, true))
    }

    @Test
    fun theOtherReasonsTheCreateButtonStaysOffStillApply() {
        assertFalse(CreateRules.canCreateProject("", "claude", "/home/me/work", false, false, true))
        assertFalse(CreateRules.canCreateProject("demo", "", "/home/me/work", false, false, true))
        assertFalse(CreateRules.canCreateProject("demo", "claude", "/home/me/work", true, false, true))
        assertFalse(CreateRules.canCreateProject("demo", "claude", "/home/me/work", false, true, true))
        // A remembered harness that the computer no longer offers is not a choice.
        assertFalse(CreateRules.canCreateProject("demo", "claude", "/home/me/work", false, false, false))
    }

    @Test
    fun thePickerStartsAtTheChosenFolderThenTheLastUsedOneThenHome() {
        assertEquals(
            "/chosen",
            CreateRules.startFolder("/chosen", listOf("/recent"), "/home/me"),
        )
        assertEquals(
            "/recent",
            CreateRules.startFolder("", listOf("/recent"), "/home/me"),
        )
        // Home is where browsing starts when there is nothing else; it is not a submission.
        assertEquals(
            "/home/me",
            CreateRules.startFolder("  ", emptyList(), "/home/me"),
        )
    }

    @Test
    fun useThisFolderOnlyActsOnThePathTheFieldStillNames() {
        // A typed path is not a choice until Go has listed it; otherwise the button would
        // confirm the previously listed folder while looking like it accepted the typing.
        assertTrue(CreateRules.canUseFolder("/home/me/work", "/home/me/work", false))
        assertTrue(CreateRules.canUseFolder("/home/me/work", " /home/me/work ", false))
        assertFalse(CreateRules.canUseFolder("/home/me/work", "/home/me/other", false))
        assertFalse(CreateRules.canUseFolder(null, "/home/me/work", false))
        assertFalse(CreateRules.canUseFolder("/home/me/work", "/home/me/work", true))
    }

    @Test
    fun onlyTheHomeFolderGetsTheTrustWarning() {
        assertTrue(CreateRules.isHomeFolder("/home/me", "/home/me"))
        assertTrue(CreateRules.isHomeFolder(" /home/me ", "/home/me"))
        // The bridge cleans the path it lists and reports home as the environment spells it.
        assertTrue(CreateRules.isHomeFolder("/home/me/", "/home/me"))
        assertFalse(CreateRules.isHomeFolder("/home/me/work", "/home/me"))
        assertFalse(CreateRules.isHomeFolder("", ""))
    }
}
