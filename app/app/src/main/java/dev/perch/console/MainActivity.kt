package dev.perch.console

import android.os.Bundle
import android.os.SystemClock
import android.content.Intent
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import dev.perch.console.core.auth.PairLink
import dev.perch.console.features.console.ConsoleScreen
import dev.perch.console.features.console.ConsoleViewModel
import dev.perch.console.features.queue.PerchNotifications
import dev.perch.console.ui.theme.PerchTheme

/**
 * The single activity.
 *
 * `adjustResize` in the manifest plus `enableEdgeToEdge` here are load-bearing
 * for the compose screen rather than cosmetic: when the soft keyboard opens, the
 * input box has to stay visible on a phone in portrait, and the IME inset is
 * what makes that happen.
 */
class MainActivity : ComponentActivity() {

    // Owned by the activity rather than created inside the composition, because
    // an incoming pairing link has to reach it before the first frame is drawn.
    private val viewModel: ConsoleViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // The moment the user's tap became this screen. Recorded here because only
        // the activity knows it, and reported later as "how long the user waited"
        // (StartupMeasurement explains why the process start is the wrong origin:
        // the watching service keeps this process alive from boot).
        viewModel.noteLaunchStart(SystemClock.uptimeMillis())
        enableEdgeToEdge()
        handlePairingLink(intent)
        handleNotificationTap(intent)
        setContent {
            PerchTheme {
                ConsoleScreen(viewModel)
            }
        }
    }

    // singleTop in the manifest means a second link arrives here instead of
    // starting a second copy of the screen.
    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        handlePairingLink(intent)
        handleNotificationTap(intent)
    }

    private fun handlePairingLink(intent: Intent?) {
        val invite = PairLink.parse(intent?.dataString) ?: return
        viewModel.offerInvite(invite)
    }

    /**
     * A tap on the body of a decision notification opens that session.
     *
     * The notification already knows which pane it is about; making the user
     * hunt for it in the project list was the gap this closes. The extras are
     * consumed so a configuration change that reuses this Intent does not reopen
     * the session and reset its scroll position.
     */
    private fun handleNotificationTap(intent: Intent?) {
        val intent = intent ?: return
        val paneId = intent.getStringExtra(PerchNotifications.EXTRA_PANE_ID)
            ?.trim()
            .orEmpty()
        if (paneId.isBlank()) return
        val host = intent.getStringExtra(PerchNotifications.EXTRA_HOST).orEmpty().trim()
        intent.removeExtra(PerchNotifications.EXTRA_PANE_ID)
        intent.removeExtra(PerchNotifications.EXTRA_HOST)
        viewModel.openSessionFromNotification(paneId, host)
    }
}
