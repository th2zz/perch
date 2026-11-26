package dev.perch.console

import android.graphics.Bitmap
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.onRoot
import androidx.compose.ui.test.performClick
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.features.terminal.SessionScreen
import dev.perch.console.ui.theme.PerchTheme
import java.io.File
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode

/**
 * The terminal screen's *layout*, since the terminal itself cannot be drawn here.
 *
 * Robolectric's WebView is a stand-in: it neither runs JavaScript nor rasterises, so the picture shows
 * the dark frame the emulator will fill and the keys underneath it. That is still worth looking at —
 * the header, the frame's share of the screen and the key row's height are exactly the things a phone
 * judges and a test cannot — and it is honest about its own limit: whether the bytes *render* is a
 * question for the device, and for the emulator's own tests.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class TerminalScreenRenderTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    @Test
    fun theTerminalFrameAndItsKeys() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
                    SessionScreen(
                        session = SessionView(
                            paneId = "wA:p1",
                            hostId = "local",
                            hostLabel = "dev-laptop",
                            workspaceLabel = "mobile agent",
                            agent = "codex",
                            displayAgent = "codex",
                            agentStatus = "working",
                            text = "an answer",
                        ),
                        busy = false,
                        onApprove = {},
                        onReject = {},
                        onSend = {},
                        onRefresh = {},
                        onClose = {},
                    )
                }
            }
        }
        compose.waitForIdle()

        // Opened the way a person opens it: the menu, then the item.
        compose.onNodeWithContentDescription("Menu").performClick()
        compose.onNodeWithText("Session screen").performClick()
        compose.waitForIdle()

        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "session-terminal.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
