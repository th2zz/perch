package dev.perch.console

import android.graphics.Bitmap
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.Host
import dev.perch.console.core.herdrclient.Mirror
import dev.perch.console.core.live.ConnectionStatus
import dev.perch.console.features.console.ConsoleActions
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.features.terminal.ShellScreen
import dev.perch.console.ui.theme.PerchTheme
import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class ShellScreenTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    private val mirror = Mirror(
        hosts = listOf(Host(hostId = Host.LOCAL, label = "dev-laptop")),
    )

    @Test
    fun theConsolesTerminalEntryIsAShellAndNotAnotherSessionScreen() {
        var opened = false
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ConsoleScreen(
                    state = ConsoleState(
                        phase = ConsoleState.Phase.Ready,
                        mirror = mirror,
                        connection = ConnectionStatus.Live,
                    ),
                    actions = ConsoleActions(openShell = { opened = true }),
                )
            }
        }
        compose.waitForIdle()

        compose.onNodeWithText("Terminal").performClick()
        compose.waitForIdle()

        // "Terminal" on the console means the shell on the computer: it is on the console rather than
        // inside a session because a shell belongs to the machine, and it must be reachable with no
        // session in front of the user.
        assertTrue("the console's Terminal entry did not open the shell", opened)
    }

    @Test
    fun theShellScreenWithNoPairing() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                Box(Modifier.fillMaxSize()) {
                    ShellScreen(
                        hostLabel = "dev-laptop",
                        // No pairing: the bridge client is absent, so this is what the screen looks like
                        // before anything is connected — the state a picture at this level can be honest
                        // about.
                        connect = { null },
                        onClose = {},
                    )
                }
            }
        }
        compose.waitForIdle()

        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "shell-screen.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
