package dev.perch.console

import android.graphics.Bitmap
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onRoot
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
 * Verifies rendering of a Codex terminal transcript containing `•` bullets,
 * collapsed tool blocks, indented continuation rows, and box-drawing separators.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class CodexTranscriptRenderTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    private val transcript = """
        • Fixed both issues and verified them in the test suite.
        • Ran "/bin/bash -lc 'cd /home/user/workspace/perch && go test ./bridge/...'"
          └ agent: codex | status: idle | chrome_lines: 4 | menu: None | unsent: None
            status-line: '┃ esc to dismiss\n? for shortcuts · high'
            body ends with: ['>', '───────────────────────────────────────────────────────────────────────────────────────────────────────────…
        •
        10 lines (^t to expand)
        • Both fixes are complete; ready to build the APK.
    """.trimIndent()

    @Test
    fun aCodexTranscriptRendersCleanly() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
                    SessionScreen(
                        session = SessionView(
                            paneId = "wA:p1",
                            hostId = "local",
                            hostLabel = "dev-laptop",
                            workspaceLabel = "perch",
                            agent = "codex",
                            displayAgent = "codex",
                            agentStatus = "working",
                            text = transcript,
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
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "codex-transcript.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
