package dev.perch.console

import android.graphics.Bitmap
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onRoot
import dev.perch.console.core.herdrclient.ConversationMessage
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.Transcript
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
 * Renders a checkpoint fixture containing both fenced code blocks and pipe tables
 * to verify card spacing and visual separation on phone widths.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class CompactCheckpointRenderTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    private val checkpoint: String =
        checkNotNull(javaClass.getResourceAsStream("/fixtures/compact-checkpoint.txt")) {
            "the fixture is missing from the test classpath"
        }.bufferedReader().readText()

    @Test
    fun theCompressedCheckpointRendersCleanly() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                Box(Modifier.fillMaxSize()) {
                    SessionScreen(
                        session = SessionView(
                            paneId = "wA:p1",
                            hostId = "local",
                            hostLabel = "dev-laptop",
                            workspaceLabel = "perch",
                            agent = "codex",
                            displayAgent = "codex",
                            agentStatus = "idle",
                            text = "the terminal's own screen, which is not what this face draws",
                        ),
                        busy = false,
                        onApprove = {},
                        onReject = {},
                        onSend = {},
                        onRefresh = {},
                        onClose = {},
                        conversation = Transcript(
                            readable = true,
                            messages = listOf(ConversationMessage("agent", checkpoint)),
                        ),
                    )
                }
            }
        }
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "compact-checkpoint.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
