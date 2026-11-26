package dev.perch.console

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.settings.Settings
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** The recent-folder list the picker starts from: newest first, no duplicates, bounded, forgettable. */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class RecentFoldersTest {

    private val settings = Settings(ApplicationProvider.getApplicationContext<Context>())

    @Test
    fun theMostRecentFolderComesFirstAndIsNotRepeated() {
        settings.rememberFolder("/home/me/work")
        settings.rememberFolder("/home/me/other")
        settings.rememberFolder("/home/me/work")

        assertEquals(listOf("/home/me/work", "/home/me/other"), settings.recentFolders)
    }

    @Test
    fun theListStaysShort() {
        for (i in 1..12) settings.rememberFolder("/home/me/project-$i")

        assertEquals(8, settings.recentFolders.size)
        assertEquals("/home/me/project-12", settings.recentFolders.first())
    }

    @Test
    fun forgettingTheComputerForgetsItsFolders() {
        settings.rememberFolder("/home/me/work")
        settings.clearRecentFolders()
        assertTrue(settings.recentFolders.isEmpty())
    }

    @Test
    fun aBlankPathIsNotRemembered() {
        settings.rememberFolder("   ")
        assertTrue(settings.recentFolders.isEmpty())
    }

    @Test
    fun aFolderNameWithANewlineIsStillOneRow() {
        // A filesystem may allow this; a separator-based list would silently turn it into two
        // bogus recent folders.
        val weird = "/home/me/two\nlines"
        settings.rememberFolder(weird)
        assertEquals(listOf(weird), settings.recentFolders)
    }
}
