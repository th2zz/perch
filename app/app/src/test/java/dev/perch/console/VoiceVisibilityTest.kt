package dev.perch.console

import androidx.compose.ui.test.assertCountEquals
import androidx.compose.ui.test.assertIsDisplayed
import androidx.compose.ui.test.junit4.v2.createComposeRule
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.onNodeWithText
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.features.queue.ComposeReply
import dev.perch.console.ui.theme.PerchTheme
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * Spec SC-011, as a test rather than as a hope.
 *
 * "With no voice credentials configured, the voice entry appears on screen zero
 * times." That was true before voice existed for the wrong reason — there was no
 * voice at all — so it could not regress and could not mean anything. Now that the
 * feature is real, the only way to keep that promise is to assert it.
 *
 * The check is on the surface where the user works: the reply box. The
 * configuration row in the settings area is deliberately not covered by this rule,
 * because without it the feature could never be turned on — and it sits with the
 * other settings rather than where instructions are written.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class VoiceVisibilityTest {

    @get:Rule
    val compose = createComposeRule()

    private val decision = Decision(
        id = "wA:p1@1",
        paneId = "wA:p1",
        workspaceLabel = "mobile agent",
        agentKind = "codex",
        promptText = "Which database should I use?",
        approvable = false,
        rejectable = false,
        answerable = true,
    )

    private fun showReplyBox(voiceConfigured: Boolean) {
        compose.setContent {
            PerchTheme(dynamicColour = false) {
                ComposeReply(
                    decision = decision,
                    busy = false,
                    voiceConfigured = voiceConfigured,
                    voiceRecording = false,
                    voiceTranscribing = false,
                    transcript = null,
                    onVoiceStart = {},
                    onVoiceStop = {},
                    onVoiceDiscard = {},
                    onSend = {},
                    onDismiss = {},
                )
            }
        }
        compose.waitForIdle()
    }

    @Test
    fun `with no key there is no way to speak, and nothing offering it`() {
        showReplyBox(voiceConfigured = false)

        compose.onAllNodesWithText("Speak instead").assertCountEquals(0)
        compose.onAllNodesWithText("Listening…").assertCountEquals(0)
        // The hint that explains voice would also be an advertisement for it.
        compose.onAllNodesWithText(
            "What you say goes into the box above. You still choose when to send it.",
        ).assertCountEquals(0)
    }

    @Test
    fun `with a key the way to speak is there`() {
        showReplyBox(voiceConfigured = true)

        compose.onNodeWithText("Speak instead").assertExists()
    }

    @Test
    fun `the box is still a text box first`() {
        // Voice is an addition, not a replacement: typing must never become the
        // fallback. This is the assertion that would fail if someone "improved" the
        // screen into a voice-first interface.
        showReplyBox(voiceConfigured = true)

        compose.onNodeWithText("Type here, then send. Nothing reaches the agent until you send.")
            .assertExists()
        compose.onNodeWithText("Send").assertExists()
        compose.onNodeWithText("Discard").assertExists()
    }
}
