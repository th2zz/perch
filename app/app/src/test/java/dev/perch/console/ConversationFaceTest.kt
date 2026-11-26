package dev.perch.console

import android.graphics.Bitmap
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
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
 * The reading face: a conversation, or the terminal's own text when there is no record to read.
 *
 * The conversation view shows only user prompts and agent responses (plus working status),
 * without exposing raw reasoning dumps.
 */
@RunWith(RobolectricTestRunner::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class ConversationFaceTest {

    @get:Rule
    val compose = createComposeRule()

    private val outputDir = File("build/screenshots").apply { mkdirs() }

    /** A session whose *screen* text is not what anyone wants to read — the chattier source. */
    private val session = SessionView(
        paneId = "wA:p1",
        hostId = "local",
        hostLabel = "dev-laptop",
        workspaceLabel = "perch",
        agent = "codex",
        displayAgent = "codex",
        agentStatus = "working",
        text = "SCREEN SCRAPED WORDS\n› esc to dismiss\n? for shortcuts · deepseek-flash · high",
        chromeLines = 2,
    )

    @androidx.compose.runtime.Composable
    private fun screen(conversation: Transcript?) {
        Box(Modifier.fillMaxSize()) {
            SessionScreen(
                session = session,
                busy = false,
                onApprove = {},
                onReject = {},
                onSend = {},
                onRefresh = {},
                onClose = {},
                conversation = conversation,
            )
        }
    }

    @Test
    fun theConversationIsTheReadingAreaWhenThereIsARecord() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(
                    Transcript(
                        readable = true,
                        messages = listOf(
                            ConversationMessage("you", "Why did my message append to the previous draft?"),
                            ConversationMessage("agent", "Because herdr types text **into** the input line rather than replacing it."),
                        ),
                    ),
                )
            }
        }
        compose.waitForIdle()

        compose.onNodeWithText("You").assertIsDisplayed()
        compose.onNodeWithText("Why did my message append to the previous draft?").assertIsDisplayed()
        // The screen's own words are not on this face at all: two renderings of the same
        // conversation, one under the other, is worse than either.
        assertNoNodeWithTextPrefix("SCREEN SCRAPED WORDS")

        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "conversation-face.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    @Test
    fun theScreensOwnTextIsUsedWhenThereIsNoRecordToRead() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(Transcript(readable = false))
            }
        }
        compose.waitForIdle()

        // An agent whose records nobody has learned is still worth opening, so the fallback is the
        // terminal's text with its furniture stripped — what this face showed before records existed.
        compose.onNodeWithText("SCREEN SCRAPED WORDS", substring = true).assertIsDisplayed()
        assertNoNodeWithTextPrefix("You")
    }

    @Test
    fun anEmptyConversationSaysSoRatherThanShowingNothing() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(Transcript(readable = true))
            }
        }
        compose.waitForIdle()
        compose.onNodeWithText("Nothing said yet.").assertIsDisplayed()
    }

    @Test
    fun aWorkingAgentIsShownWithHowLongItHasBeenGoing() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(
                    Transcript(
                        readable = true,
                        messages = listOf(ConversationMessage("you", "Fix the reconnect backoff")),
                        working = true,
                        workingSecs = 150,
                    ),
                )
            }
        }
        compose.waitForIdle()
        compose.onNodeWithText("working · 2 minutes").assertIsDisplayed()
    }

    @Test
    fun workInProgressWithNoMeasuredDurationClaimsNoDuration() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(Transcript(readable = true, working = true, workingSecs = 0))
            }
        }
        compose.waitForIdle()
        // "working", with no number: a stopwatch the phone made up would be a claim about the
        // computer's time that the phone cannot make.
        compose.onNodeWithText("working").assertIsDisplayed()
    }

    @Test
    fun theStartOfALongConversationIsMarkedWhenItIsMissing() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(
                    Transcript(
                        readable = true,
                        messages = listOf(ConversationMessage("agent", "Continuing from above.")),
                        truncated = true,
                    ),
                )
            }
        }
        compose.waitForIdle()
        compose
            .onNodeWithText("Older output is not shown — the rest is still on your computer.")
            .assertIsDisplayed()
    }

    @Test
    fun aMessageFromAnUnknownSpeakerIsNotShownAsTheUsersOwnWords() {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                screen(
                    Transcript(
                        readable = true,
                        messages = listOf(ConversationMessage("system", "the tool was interrupted")),
                    ),
                )
            }
        }
        compose.waitForIdle()
        compose.onNodeWithText("the tool was interrupted").assertIsDisplayed()
        // Under "You" would be a claim that the user wrote it, and this face must never put words in
        // their mouth — a renamed role on the bridge is a wrong screen, not a missing one.
        assertNoNodeWithTextPrefix("You")
    }

    /**
     * Verifies that a wrapped user message drawn with a leading `>` and an indented
     * continuation line renders cleanly inside the fallback view without splitting the
     * continuation line outside the user's message.
     */
    @Test
    fun aWrappedUserMessageOnAGyStaysOneMessage() {
        val agy = SessionView(
            paneId = "wD:p1",
            hostId = "local",
            hostLabel = "dev-laptop",
            workspaceLabel = "perch",
            agent = "agy",
            displayAgent = "agy",
            agentStatus = "idle",
            text = """
                ────────────────────────────────────────────────────────────
                > Keep the bridge stateless on disk except for the pairing token and local queue state,
                  and make sure the phone can reconnect cleanly after the network drops.

                ▸ Thought for 12s, 1.4k tokens
                  Sounds good. I will keep state minimal and add reconnect coverage.
            """.trimIndent(),
            chromeLines = 1,
        )

        compose.setContent {
            PerchTheme(dynamicColour = false) {
                Box(Modifier.fillMaxSize()) {
                    SessionScreen(
                        session = agy,
                        busy = false,
                        onApprove = {},
                        onReject = {},
                        onSend = {},
                        onRefresh = {},
                        onClose = {},
                        // No record to read, which is what agy is today: the fallback face.
                        conversation = Transcript(readable = false),
                    )
                }
            }
        }
        compose.waitForIdle()

        compose.onNodeWithText("and make sure the phone can reconnect cleanly after the network drops.", substring = true)
            .assertIsDisplayed()

        val image = compose.onRoot().captureToImage().asAndroidBitmap()
        File(outputDir, "conversation-fallback-agy.png").outputStream().use { out ->
            image.compress(Bitmap.CompressFormat.PNG, 100, out)
        }
    }

    private fun assertNoNodeWithTextPrefix(prefix: String) {
        val matches = compose.onAllNodesWithText(prefix, substring = true).fetchSemanticsNodes().size
        if (matches > 0) {
            throw AssertionError("\"$prefix\" is on the reading face but should not be ($matches nodes)")
        }
    }
}
