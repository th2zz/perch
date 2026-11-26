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
 * Renders a box-drawn terminal table with CJK cells at phone width to verify
 * display-cell column alignment.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class DrawnTableRenderTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    /** Written the way a terminal writes it: columns padded, borders split per column. */
    private val transcript = listOf(
        "  Verified capabilities (each backed by a repeatable check)",
        "",
        "   Capability" + " ".repeat(24) + "Verification",
        "  " + "━".repeat(34) + "  " + "━".repeat(46),
        "   Composer guard (unsent prompt)" + " ".repeat(4) + "scripts/verify-composer-guard.sh 6/6",
        "  " + "─".repeat(34) + "  " + "─".repeat(46),
        "   Live refresh (0.8s / 3s)" + " ".repeat(10) + "LiveSessionTest (median 9-19ms)",
        "  " + "─".repeat(34) + "  " + "─".repeat(46),
        "   Auto-reconnect on resume" + " ".repeat(10) + "bridge heartbeat 9 req/min",
        "  " + "─".repeat(34) + "  " + "─".repeat(46),
        "",
        "  Trailing paragraph below the table.",
    ).joinToString("\n")

    @Test
    fun aTableTheTerminalDrewReadsAsATable() {
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
        File(outputDir, "session-drawn-table.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }
}
