package dev.perch.console.features.terminal

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
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
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
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import dev.perch.console.R
import dev.perch.console.core.herdrclient.TerminalEvents
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.launch

@Composable
fun ShellScreen(
    hostLabel: String,
    modifier: Modifier = Modifier,
    /**
     * A connection factory, or null when the phone is not paired.
     *
     * `fresh` is what a person means by "new terminal": end the shell that is running and start another.
     * A factory rather than a connection because a reconnect has to be a *new* socket (the old one is
     * gone) while the shell stays the same — which is the whole reason this is safe to do at all.
     */
    connect: (fresh: Boolean) -> ShellConnection? = { null },
    onClose: () -> Unit,
    /**
     * One line about this screen, for the computer's log.
     *
     * The terminal is the only screen whose failures are invisible from the host — a black rectangle
     * happens inside a WebView — so the phone says what it did and what it saw.
     */
    onReport: (event: String, detail: String) -> Unit = { _, _ -> },
    /** Sends a picture of this screen to the user's own computer, when they press the button. */
    onSendScreenshot: (png: ByteArray) -> Unit = {},
) {
    val context = LocalContext.current
    /*
     * Reconnecting is a new *socket*, never a new page.
     *
     * This is the trap that cost two days on the pane screen, and it was in the first version of this one
     * too: a `remember(attempt) { connect(...) }` makes every retry a new connection object, and
     * `DisposableEffect(connection) { web?.destroy() }` then tears the WebView down the instant the page
     * finishes loading (which is itself what triggers the first re-attach). The result is a terminal that
     * is dark forever, with the bridge's log showing bytes sent and nothing drawn.
     *
     * So the connection is *state*, not a composition key, and the page is destroyed only when the screen
     * itself leaves. Everything that needs the current socket reads it here.
     */
    var connection by remember { mutableStateOf<ShellConnection?>(null) }
    var attempt by remember { mutableIntStateOf(0) }
    var wantedFresh by remember { mutableStateOf(false) }
    // The shell's end, when there is nothing to come back to (see ShellConnection.Event.Ended.gone).
    var gone by remember { mutableStateOf<String?>(null) }
    // Whether this screen is between connections because the network went away.
    var reconnecting by remember { mutableStateOf(false) }
    var pageReady by remember { mutableStateOf(false) }
    var web by remember { mutableStateOf<WebView?>(null) }
    // The grid the page last measured, kept here because it is reported *before* the socket exists: the
    // page says how big the glass is as soon as it is told, while the stream is opened a moment later. A
    // resize sent to a socket that is not there yet is dropped, and the shell would keep drawing at the
    // bridge's default 80x24.
    var phoneCols by remember { mutableIntStateOf(0) }
    var phoneRows by remember { mutableIntStateOf(0) }
    var sawBytes by remember { mutableStateOf(false) }
    val main = remember { Handler(Looper.getMainLooper()) }

    val bridge = remember {
        TerminalPage(
            send = { connection?.send(it) },
            report = { event, detail -> main.post { onReport(event, detail) } },
            // The page's own grid, straight onto the socket: this is a terminal the phone owns, so its
            // size is what the shell has to be told (and what makes a full-screen program redraw).
            size = { cols, rows ->
                phoneCols = cols
                phoneRows = rows
                connection?.resize(cols, rows)
            },
        )
    }

    LaunchedEffect(connection, pageReady) {
        gone = null
        if (!pageReady) return@LaunchedEffect
        val link = connection ?: return@LaunchedEffect
        onReport(TerminalEvents.OPENED, "shell attempt $attempt fresh=$wantedFresh")
        // The shape goes first, for the reason it was remembered: whatever the page measured before this
        // socket existed is what this shell has to be.
        if (phoneCols > 0 && phoneRows > 0) {
            link.resize(phoneCols, phoneRows)
        }
        var firstBytes = true
        link.events().collectLatest { event ->
            when (event) {
                is ShellConnection.Event.Bytes -> {
                    if (firstBytes) {
                        firstBytes = false
                        sawBytes = true
                        reconnecting = false
                        onReport(TerminalEvents.BYTES, "first ${event.payload.size} bytes")
                    }
                    val encoded = Base64.encodeToString(event.payload, Base64.NO_WRAP)
                    web?.evaluateJavascript("PerchTerminal.write('$encoded')", null)
                }

                is ShellConnection.Event.Ended -> {
                    if (event.gone) {
                        // The shell itself stopped: reattaching would fetch the same sentence again, so
                        // the only useful thing left is a new terminal.
                        onReport(TerminalEvents.ENDED, event.reason)
                        reconnecting = false
                        gone = event.reason
                        return@collectLatest
                    }
                    // The connection broke, the shell did not. Come back to it — this is the whole of
                    // what "mosh-like" means here, and it is why the screen says "reconnecting" rather
                    // than showing the user a failure to respond to.
                    reconnecting = true
                    onReport(TerminalEvents.ENDED, "${event.reason} — reattaching in attempt ${attempt + 1}")
                    delay(ShellRetry.delayFor(attempt))
                    // Never `fresh`: a phone that lost the network must not take the running shell with
                    // it. Only the two buttons in the header do that.
                    wantedFresh = false
                    attempt += 1
                    connection = connect(false)
                }
            }
        }
    }

    // The same check the pane screen makes, for the same reason: bytes arriving and pixels existing are
    // two different questions, and a black rectangle is a question about the second one. Counted from
    // actual pixels, once per open, a couple of seconds after the first bytes.
    LaunchedEffect(attempt, pageReady, sawBytes) {
        if (!pageReady || !sawBytes) return@LaunchedEffect
        val activity = context.findActivity() ?: return@LaunchedEffect
        delay(2_500)
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
                    text = stringResource(R.string.terminal_title, hostLabel),
                    style = MaterialTheme.typography.titleSmall,
                    modifier = Modifier.weight(1f),
                )
                // Offered whenever the terminal has stopped, and always available when it has not: "the
                // one I have is stuck" is the usual reason to want another one.
                TextButton(onClick = {
                    wantedFresh = true
                    reconnecting = false
                    attempt += 1
                    connection = connect(true)
                }) {
                    Text(stringResource(R.string.terminal_new))
                }
                TextButton(onClick = onClose) { Text(stringResource(R.string.session_close)) }
                // "Send screen" is behind the menu rather than in the header, and the reason is the one
                // this whole product keeps running into: a terminal is short of rows, and a button that
                // floats over the first two of them costs more than it explains. The pane screen keeps the
                // same button in the same place, so the two terminals are operated the same way.
                var menuOpen by remember { mutableStateOf(false) }
                val activity = context.findActivity()
                val scope = rememberCoroutineScope()
                Box {
                    TextButton(onClick = { menuOpen = true }) {
                        Text(
                            text = "⋮",
                            style = MaterialTheme.typography.titleLarge,
                            modifier = Modifier.semantics { contentDescription = "Terminal menu" },
                        )
                    }
                    DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text(stringResource(R.string.terminal_send_screen)) },
                            onClick = {
                                menuOpen = false
                                scope.launch {
                                    val png = activity?.let { ScreenCapture.windowPng(it) }
                                    if (png != null) onSendScreenshot(png)
                                    else onReport(TerminalEvents.ERROR, "could not capture the screen")
                                }
                            },
                        )
                    }
                }
            }

            Box(modifier = Modifier.weight(1f)) {
                AndroidView(
                    modifier = Modifier.fillMaxSize(),
                    factory = { viewContext ->
                        terminalWebView(
                            context = viewContext,
                            bridge = bridge,
                            // The phone owns the shape: the page measures its own glass and reports the
                            // grid, and the shell is resized to it.
                            ownSize = true,
                            onPageLoaded = {
                                // A page that has just loaded — first time, or after a rotation, which
                                // recreates the view — is a blank emulator, and the shell will not repaint
                                // by itself, because a pseudo-terminal has no idea anybody is looking.
                                // Re-attaching makes the bridge replay the recent output, which is exactly
                                // what redraws it.
                                main.post {
                                    pageReady = true
                                    // Reconnecting after a rotation is a new socket, never a new shell —
                                    // unless the user pressed New before the page was even up.
                                    connection = connect(wantedFresh)
                                    wantedFresh = false
                                    attempt += 1
                                }
                            },
                        ).also { view ->
                            web = view
                            // The page's own viewport can be a lie, so the side that knows the size says
                            // so — and it is what makes the glass real before the first measurement.
                            view.followItsOwnSize(bridge)
                        }
                    },
                )
                // One line at the bottom of the terminal, and only when there is something true to say:
                // "reconnecting" is a state, not a failure, and it is the difference between a terminal
                // that behaves like mosh and one that looks broken in a tunnel.
                val line = when {
                    gone != null -> stringResource(R.string.terminal_ended, gone ?: "")
                    reconnecting -> stringResource(R.string.terminal_reconnecting)
                    else -> null
                }
                if (line != null) {
                    Text(
                        text = line,
                        color = if (gone != null) {
                            MaterialTheme.colorScheme.error
                        } else {
                            MaterialTheme.colorScheme.onSurfaceVariant
                        },
                        style = MaterialTheme.typography.labelSmall,
                        modifier = Modifier
                            .align(Alignment.BottomStart)
                            .padding(8.dp),
                    )
                }
            }

            // The keys a phone keyboard does not have, sent straight to the shell as bytes. Here they are
            // not an escape hatch around somebody else's dialog: this is our terminal, and Ctrl+C is how a
            // person stops something they started.
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

    // Keyed on the screen, not on the connection: see the note where `connection` is declared. A page torn
    // down mid-session is a terminal that is dark forever, and the log would show bytes arriving.
    DisposableEffect(Unit) {
        onDispose { web?.destroy() }
    }
}

/**
 * How long to wait before attaching again.
 *
 * A ladder rather than a fixed pause or an unbounded exponential: the first retry has to be fast enough
 * that a phone switching from Wi-Fi to cellular looks like a blink, and the last one has to be slow
 * enough that a phone in a tunnel for an hour is not making a request every two seconds for an hour.
 * Both ends are the point, so both are written down and tested; the cap is 8 seconds, which is slower
 * than a person's patience with a blank terminal but far cheaper than a phone's battery.
 */
internal object ShellRetry {
    private val ladder = longArrayOf(1_000, 2_000, 4_000, 8_000)

    fun delayFor(attempt: Int): Long = ladder[attempt.coerceIn(0, ladder.size - 1)]
}
