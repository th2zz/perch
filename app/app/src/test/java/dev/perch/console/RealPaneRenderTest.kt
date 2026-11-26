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

@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class RealPaneRenderTest {

    @get:Rule
    val compose = createComposeRule()

    private val transcript = """  • Fixed the layout issue and verified both cases in the unit suite.
  • Ran "/bin/bash -lc 'cd /home/user/workspace/perch && ./scripts/check-terminal-page.sh'"
    └ agent: codex | status: idle | chrome_lines: 4 | menu: None | unsent: None
      status-line: '┃ esc to dismiss\n? for shortcuts · high'
      body ends with: ['>',
  '────────────────────────────────────────────────────────────────────────────────────────────────────────────────…
  •
  10 lines (^t to expand)
  • Both checks are green; building the release APK next.


• Added app/app/src/test/java/dev/perch/console/CodexTranscriptRenderTest.kt (+89 -0)
     1 +package dev.perch.console
     2 +
     3 +import android.graphics.Bitmap
    + Show details

• Ran cd /home/user/workspace/perch/app && ./gradlew :app:testDebugUnitTest
  └ BUILD SUCCESSFUL in 12s
    -rw-r--r-- 1 user user 135655 Oct 14 08:08 app/build/screenshots/codex-transcript.png
    + Show details


› Check how the rendered markdown looks in the screenshot.


• Viewed image codex-transcript.png

• Verified against the rendered pane output.


› Ask Codex to do anything

  codex high · Context 58% used · 6.97M used
  ← for agents · ? for shortcuts
"""

    @Test
    fun theRealPaneRenders() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                androidx.compose.foundation.layout.Box(Modifier.fillMaxSize()) {
                    SessionScreen(
                        session = SessionView(
                            paneId = "wA:p1",
                            agent = "codex",
                            displayAgent = "codex",
                            agentStatus = "working",
                            workspaceLabel = "perch",
                            text = transcript,
                        ),
                        busy = false, onApprove = {}, onReject = {},
                        onSend = {}, onRefresh = {}, onClose = {},
                    )
                }
            }
        }
        compose.waitForIdle()
        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File("build/screenshots").apply { mkdirs() }.let { dir ->
            File(dir, "real-pane.png").outputStream().use { image.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }
}
