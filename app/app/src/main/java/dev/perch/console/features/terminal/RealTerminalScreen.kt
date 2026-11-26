package dev.perch.console.features.terminal

import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.webkit.WebView
import androidx.compose.foundation.background
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.AssistChip
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import dev.perch.console.R
import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.TerminalEvents
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

@SuppressLint("SetJavaScriptEnabled")
@Composable
fun RealTerminalScreen(
    session: SessionView,
    modifier: Modifier = Modifier,
    /**
     * A fresh connection, or null when the phone is not paired. A factory rather than a connection so
     * that closing and reopening the screen gets a new socket — and with it, a fresh repaint of the
     * screen as it is *now*, which is the whole reason a reconnect is safe here.
     */
    connect: () -> TerminalConnection?,
    onClose: () -> Unit,
    /**
     * One line about this screen, for the computer's log.
     *
     * The terminal is the only screen whose failures are invisible from the host: everything else leaves a
     * trace the bridge can read afterwards, but a black terminal happens inside a WebView. So the phone
     * tells the computer what it did and what it saw, and a report is a log line rather than a state change
     * (which is why it is fire-and-forget and never waited on).
     */
    onReport: (event: String, detail: String) -> Unit = { _, _ -> },
    /**
     * Sends a picture of this screen to the computer, when the user presses the button.
     *
     * The button exists because a screen that is black while everything behind it claims to be fine is a
     * question about pixels, and words are a bad instrument for pixels.
     */
    onSendScreenshot: (png: ByteArray) -> Unit = {},
) {
    val context = LocalContext.current
    val connection = remember(session.paneId) { connect() }
    // What the platform knows about this view, once it has been laid out: a black terminal is either a
    // page that never ran, a box with no size, or a view that is not on screen — and this line is the
    // second of those three answered from the side that owns the view.
    val viewFacts: (android.webkit.WebView, String) -> Unit = { view, when_ ->
        onReport(
            TerminalEvents.VIEW,
            "$when_ webview ${view.width}x${view.height} density " +
                "${view.resources.displayMetrics.density} attached ${view.isAttachedToWindow} " +
                "visibility ${view.visibility}",
        )
    }
    var ended by remember(session.paneId) { mutableStateOf<String?>(null) }
    // Whether the stream has delivered anything, and — a moment later — whether the screen really shows it.
    var sawBytes by remember(session.paneId) { mutableStateOf(false) }
    var attempt by remember(session.paneId) { mutableIntStateOf(0) }
    val main = remember { Handler(Looper.getMainLooper()) }

    // The page's own view of the screen: what it reports becomes the host terminal's size, and what
    // the app receives from the socket is written back into it.
    val bridge: TerminalPage = remember(session.paneId) {
        TerminalPage(
            send = { connection?.send(it) },
            report = { event, detail -> main.post { onReport(event, detail) } },
        )
    }

    var web by remember(session.paneId) { mutableStateOf<WebView?>(null) }
    var pageReady by remember(session.paneId) { mutableStateOf(false) }
    // The pane's shape, remembered rather than only forwarded.
    //
    // The bridge sends it the instant the stream attaches — which is usually *before* the WebView has
    // finished loading its page, and an `evaluateJavascript` into a page that has not run yet is thrown
    // away without an error. So the shape is kept here and re-applied when the page says it is ready
    // (and again after a rotation, which loads a fresh page). Without this, the terminal draws a
    // 183-column screen into whatever grid it defaulted to.
    var hostCols by remember(session.paneId) { mutableIntStateOf(0) }
    var hostRows by remember(session.paneId) { mutableIntStateOf(0) }
    val showHostScreen: (Int, Int) -> Unit = { cols, rows ->
        hostCols = cols
        hostRows = rows
        web?.evaluateJavascript("PerchTerminal.showHostScreen($cols, $rows)", null)
    }

    LaunchedEffect(connection, attempt, pageReady) {
        ended = null
        if (!pageReady) return@LaunchedEffect
        val link = connection ?: return@LaunchedEffect
        onReport(TerminalEvents.OPENED, "attach ${session.paneId} attempt $attempt")
        var firstBytes = true
        link.events().collectLatest { event ->
            when (event) {
                is TerminalConnection.Event.Bytes -> {
                    sawBytes = true
                    if (firstBytes) {
                        firstBytes = false
                        onReport(TerminalEvents.BYTES, "first ${event.payload.size} bytes")
                    }
                    val encoded = Base64.encodeToString(event.payload, Base64.NO_WRAP)
                    web?.evaluateJavascript("PerchTerminal.write('$encoded')", null)
                }
                // The pane's own size, sent by the bridge as it attaches: the emulator draws *that*
                // screen — the same one the person at the computer is looking at — rather than the
                // phone's shape, which the pane would have to be reshaped into.
                is TerminalConnection.Event.Shape -> {
                    onReport(TerminalEvents.SHAPE, "${event.cols}x${event.rows}")
                    showHostScreen(event.cols, event.rows)
                }
                is TerminalConnection.Event.Ended -> {
                    onReport(TerminalEvents.ENDED, event.reason)
                    ended = event.reason
                }
            }
        }
    }

    LaunchedEffect(session.paneId, attempt, pageReady, sawBytes) {
        if (!pageReady || !sawBytes) return@LaunchedEffect
        val activity = context.findActivity() ?: return@LaunchedEffect
        kotlinx.coroutines.delay(2_500)
        val stats = ScreenCapture.windowStats(activity) ?: return@LaunchedEffect
        onReport(TerminalEvents.VIEW, ScreenCapture.describe(stats))
    }

    Surface(modifier = modifier.fillMaxSize()) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .windowInsetsPadding(WindowInsets.safeDrawing)
                .background(MaterialTheme.colorScheme.surface),
        ) {
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 8.dp, vertical = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Text(
                    text = stringResource(R.string.terminal_title, session.agentLabel),
                    style = MaterialTheme.typography.titleSmall,
                    modifier = Modifier.weight(1f),
                )
                // "Send screen": the terminal's own header, one tap, no dialog. The picture goes to the
                // user's own computer over their own tailnet, and lands next to the bridge's state where
                // they can look at it too.
                // The Activity, found by unwrapping the context rather than casting it blind: the context
                // a Compose screen sees can be a ContextWrapper, and PixelCopy needs the real Activity.
                // (This project's activity-compose predates `LocalActivity`; the helper is the same idea.)
                val activity = context.findActivity()
                val scope = rememberCoroutineScope()
                TextButton(onClick = {
                    if (activity != null) {
                        scope.launch {
                            val png = ScreenCapture.windowPng(activity)
                            if (png != null) onSendScreenshot(png)
                            else onReport(TerminalEvents.ERROR, "could not capture the screen")
                        }
                    }
                }) {
                    Text(stringResource(R.string.terminal_send_screen))
                }
                if (ended != null) {
                    TextButton(onClick = { attempt += 1 }) {
                        Text(stringResource(R.string.terminal_reconnect))
                    }
                }
                TextButton(onClick = onClose) { Text(stringResource(R.string.session_close)) }
            }

            Box(modifier = Modifier.weight(1f)) {
                AndroidView(
                    modifier = Modifier.fillMaxSize(),
                    factory = { context ->
                        terminalWebView(
                            context = context,
                            bridge = bridge,
                            // A pane belongs to a program somebody is looking at on the computer, so the
                            // phone draws *its* shape and never reports one (D37). The shell screen is the
                            // opposite and passes true.
                            ownSize = false,
                            onPageLoaded = { loaded ->
                                // A page that has just loaded — first time, or after a rotation which
                                // recreates the whole view — is a blank emulator, and the terminal on
                                // the host will not redraw by itself. Attaching again always makes it
                                // repaint the *current* screen, so that is what a fresh page does.
                                // Rotation used to end in a black terminal for exactly this reason.
                                // The shape goes with it, from what the stream already told us.
                                main.post {
                                    pageReady = true
                                    if (hostCols > 0 && hostRows > 0) {
                                        loaded.evaluateJavascript(
                                            "PerchTerminal.showHostScreen($hostCols, $hostRows)",
                                            null,
                                        )
                                    }
                                    attempt += 1
                                }
                            },
                        ).also { view ->
                            web = view
                            view.post { viewFacts(view, "laid out") }
                            // The page's own viewport can be a lie, so the side that knows the size says
                            // so — and keeps saying it as the view changes. Shared with the shell screen,
                            // which draws through the same box (see [followItsOwnSize]).
                            view.followItsOwnSize(bridge)
                        }
                    },
                )
                if (ended != null) {
                    Text(
                        text = stringResource(R.string.terminal_ended, ended ?: ""),
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier
                            .align(Alignment.BottomStart)
                            .padding(8.dp),
                    )
                }
            }

            // The keys a phone keyboard does not have. Escape, Tab, arrows and Ctrl are how a dialog
            // is answered, and they type into the terminal exactly as if the keyboard had them.
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 8.dp, vertical = 4.dp),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                for (key in TerminalKeys.all) {
                    AssistChip(
                        onClick = { connection?.send(key.bytes) },
                        label = { Text(key.label) },
                        modifier = Modifier.heightIn(min = 40.dp),
                    )
                }
            }
        }
    }

    DisposableEffect(connection) {
        onDispose { web?.destroy() }
    }
}

/**
 * The Activity behind whatever context a Compose screen was given.
 *
 * Unwrapping rather than casting, because the context of a composable inside an Activity is often a
 * ContextWrapper and a blind cast is both wrong and exactly what lint objects to. PixelCopy will only take
 * the real thing.
 */
internal tailrec fun android.content.Context.findActivity(): android.app.Activity? = when (this) {
    is android.app.Activity -> this
    is android.content.ContextWrapper -> baseContext.findActivity()
    else -> null
}

