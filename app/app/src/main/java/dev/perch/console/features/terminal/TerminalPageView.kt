package dev.perch.console.features.terminal

import android.annotation.SuppressLint
import android.content.Context
import android.util.Base64
import android.webkit.JavascriptInterface
import android.webkit.WebView
import android.webkit.WebViewClient
import dev.perch.console.core.herdrclient.TerminalEvents

/**
 * The one WebView both terminals are drawn in, and the calls between it and the app.
 *
 * There are two terminals, and they are different things drawn by the same renderer:
 *
 *   - **a pane herdr owns** ([RealTerminalScreen]) — the phone is a viewer, the computer's shape is the
 *     truth, and the page is told `showHostScreen(cols, rows)`;
 *   - **a shell of the phone's own** ([ShellScreen]) — the phone owns the shape, so the page measures its
 *     own glass and reports the grid back, which the app turns into a resize on the socket.
 *
 * Sharing the page is the point: xterm.js is vendored once, the emulator behaves the same way in both,
 * and the seam between Kotlin and JavaScript — names in both directions, with no compiler to check them —
 * is written down once and checked by `scripts/terminal-page/terminal-contract.test.js`. `ownSize` is the only
 * difference, and it is named rather than positional so a reader can see which one they are looking at.
 */
@SuppressLint("SetJavaScriptEnabled")
internal fun terminalWebView(
    context: Context,
    bridge: TerminalPage,
    ownSize: Boolean,
    onPageLoaded: (WebView) -> Unit,
): WebView {
    val view = WebView(context)
    view.settings.javaScriptEnabled = true
    // The page is a local asset and nothing else: no network, no zoom, no scrolling of the page itself
    // (the terminal has its own). Every flag here is about keeping it that way.
    view.settings.allowFileAccess = false
    view.settings.allowContentAccess = false
    view.settings.setSupportZoom(false)
    view.settings.builtInZoomControls = false
    view.setBackgroundColor(0xFF101014.toInt())
    view.addJavascriptInterface(bridge, "PerchAndroid")
    view.webViewClient = object : WebViewClient() {
        override fun onPageFinished(loaded: WebView, url: String) {
            // Order matters, and it is stated once, here.
            //
            // Whose shape this page draws comes first: a shell whose grid nobody has reported would be
            // started by the bridge at its default 80x24 and resized a moment later, which is a visible
            // reflow of everything already on the screen.
            if (ownSize) {
                loaded.evaluateJavascript("PerchTerminal.ownSize()", null)
            }
            // Then the size, and *here* rather than in `view.post`, because this is the first moment the
            // page's script exists: a JavaScript call into a page that has not run yet is dropped in
            // silence, and that is exactly what the measurements show — three pushes logged before
            // `page`, and a box still six pixels tall (2026-10-15).
            pushGlass(bridge, loaded)
            // Then `ready()`, which is what opens the stream.
            loaded.evaluateJavascript("PerchTerminal.ready()", null)
            onPageLoaded(loaded)
        }
    }
    view.loadUrl("file:///android_asset/terminal/index.html")
    return view
}

/**
 * Hands the page its own size, in the units CSS uses, and keeps handing it over as the view changes.
 *
 * The page's own viewport can be a lie: a WebView can lay a page out before it has its final size and
 * then never reflow it. Measured on the emulator (2026-10-15): without this the page reports
 * `glass 393x6` while the view is 393x691 CSS pixels, and every row it draws is clipped to black. Two
 * triggers, because one is not enough — the page can finish loading before the view is laid out, and the
 * view can change size afterwards (a rotation, a keyboard opening).
 *
 * Shared by both terminals so that neither can have this fix and the other forget it: the black screen
 * was a pane-view bug, and the shell screen draws through exactly the same box.
 */
internal fun WebView.followItsOwnSize(bridge: TerminalPage) {
    post { pushGlass(bridge, this) }
    addOnLayoutChangeListener { changed, left, top, right, bottom, oldLeft, oldTop, oldRight, oldBottom ->
        if (right - left != oldRight - oldLeft || bottom - top != oldBottom - oldTop) {
            pushGlass(bridge, changed as WebView)
        }
    }
}

private fun pushGlass(bridge: TerminalPage, view: WebView) {
    val density = view.resources.displayMetrics.density.takeIf { it > 0f } ?: 1f
    val cssWidth = Math.round(view.width / density)
    val cssHeight = Math.round(view.height / density)
    bridge.onTerminal(
        TerminalEvents.VIEW,
        "webview ${view.width}x${view.height} = ${cssWidth}x${cssHeight} css (density $density)",
    )
    view.evaluateJavascript("PerchTerminal.setGlass($cssWidth, $cssHeight)", null)
}

/**
 * The page's calls into the app.
 *
 * A named class rather than an object expression: an object expression works at runtime, but a type with
 * no name gives a reader (and a checker) nothing to hold on to — and Android lint reports an object
 * expression as "none of the methods have been annotated with @JavascriptInterface", as a
 * build-failing error, about methods that are annotated. Named, the contract is in one place.
 */
internal class TerminalPage(
    private val send: (ByteArray) -> Unit,
    private val report: (event: String, detail: String) -> Unit,
    /**
     * The page's own grid, for a terminal the phone owns.
     *
     * A shell on the computer has no shape until the phone measures its glass and says so — so this is
     * data rather than a log line, and the screen turns it into a resize on the socket. The pane viewer
     * has no use for it (it draws the computer's shape, not its own) and passes nothing.
     */
    private val size: (cols: Int, rows: Int) -> Unit = { _, _ -> },
) {
    @JavascriptInterface
    fun onData(base64: String) {
        val bytes = runCatching { Base64.decode(base64, Base64.DEFAULT) }.getOrNull() ?: return
        send(bytes)
    }

    @JavascriptInterface
    fun onTerminal(event: String, detail: String) {
        report(event, detail)
    }

    @JavascriptInterface
    fun onSize(cols: Int, rows: Int) {
        size(cols, rows)
    }
}
