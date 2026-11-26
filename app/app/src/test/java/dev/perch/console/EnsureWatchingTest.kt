package dev.perch.console

import android.app.Application
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.live.BridgeStream
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.features.console.ConsoleViewModel
import dev.perch.console.ui.theme.PerchTheme
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * "Watching: on" has to mean something is watching — including after a force stop.
 *
 * The first real trial found the hole (#52): the service was started on pair, on
 * the toggle and at boot, and *not* when the app opened. So after Android's "force
 * stop" the user could open the app, see the switch on, and be watched by nothing at
 * all: the next agent would block and the phone would stay silent, which is the
 * failure this product exists to prevent.
 *
 * Two halves, tested apart because they can fail apart: the screen has to ask, and
 * the state holder has to act. The third half — the service installing its
 * collectors only once, so asking repeatedly is harmless — is in the service, where
 * a second start now re-reads the queue instead of stacking another collector.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class EnsureWatchingTest {

    @get:Rule
    val compose = createComposeRule()

    private val app: PerchApp = ApplicationProvider.getApplicationContext()

    @After
    fun stopStream() {
        BridgeStream.stop()
    }

    private fun render(phase: ConsoleState.Phase, watching: Boolean, onEnsure: () -> Unit) {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(phase = phase, watchInBackground = watching),
                    actions = ConsoleActions(ensureWatching = onEnsure),
                )
            }
        }
    }

    @Test
    fun showingAConnectedComputerMakesSureSomethingIsWatching() {
        var ensured = 0
        render(ConsoleState.Phase.Ready, watching = true) { ensured++ }
        compose.waitForIdle()

        assertEquals("the screen must ask for the service to be ensured", 1, ensured)
    }

    @Test
    fun theUsersChoiceToNotWatchInTheBackgroundIsRespected() {
        var ensured = 0
        render(ConsoleState.Phase.Ready, watching = false) { ensured++ }
        compose.waitForIdle()

        assertEquals("ensuring a service the user turned off would be a surprise notification", 0, ensured)
    }

    @Test
    fun nothingIsEnsuredBeforeThereIsAComputer() {
        var ensured = 0
        render(ConsoleState.Phase.NeedsPairing, watching = true) { ensured++ }
        compose.waitForIdle()

        assertEquals("an unpaired phone must not get a permanent notification", 0, ensured)
    }

    @Test
    fun theStateHolderStartsTheServiceWhenThePreferenceIsOn() {
        app.container.settings.watchInBackground = true
        ConsoleViewModel(app).ensureWatching()

        assertNotNull(
            "with the preference on, opening the app must bring the service back",
            shadowOf(app as Application).nextStartedService,
        )
    }

    @Test
    fun theStateHolderLeavesItAloneWhenThePreferenceIsOff() {
        app.container.settings.watchInBackground = false
        ConsoleViewModel(app).ensureWatching()

        assertNull(
            "off means off: no service, and no permanent notification",
            shadowOf(app as Application).nextStartedService,
        )
    }
}
