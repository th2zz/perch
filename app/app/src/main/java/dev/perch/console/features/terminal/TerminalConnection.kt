package dev.perch.console.features.terminal

import dev.perch.console.core.herdrclient.BridgeClient
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

/**
 * The phone's end of a real terminal.
 *
 * The wire was designed and verified on the bridge side first (`scripts/verify-terminal.sh` drives it with
 * a hand-written client, and types a command the pane really runs), so this file is only the client:
 *
 *   binary frame, out   the terminal's own bytes, straight to the emulator
 *   binary frame, in    what the user typed
 *   text frame,   in    a control message — `{"type":"resize","cols":…,"rows":…}`
 *
 * Two properties are worth stating because they are why this is a small file rather than a session
 * manager. **Nothing here owns the terminal**: the pane belongs to herdr and to the person at the
 * computer, so a closed socket ends nothing, and the next attach repaints the screen. And **nothing is
 * buffered**: bytes that arrive while the emulator is still loading are dropped rather than queued,
 * because the attach that follows a reconnect always sends the whole screen — replaying a stale one
 * would draw history over the truth.
 */
class TerminalConnection(
    private val client: BridgeClient,
    private val paneId: String,
    private val hostId: String,
) {

    /**
     * What the terminal sends us: raw bytes, the pane's shape, or a notice that the stream ended.
     *
     * [Shape] is the bridge's own message (it is not part of herdr's protocol) and it is what makes the
     * phone a *viewer* of the pane rather than a second owner of it: the pane's terminal is one
     * terminal, drawn at one size, and the phone draws that size instead of imposing its own.
     */
    sealed interface Event {
        data class Bytes(val payload: ByteArray) : Event
        data class Shape(val cols: Int, val rows: Int) : Event
        data class Ended(val reason: String) : Event
    }

    /**
     * The stream, as a flow.
     *
     * Cold and self-closing: the flow owns the socket for as long as it is collected, which is exactly
     * as long as the screen showing it is composed. When the user closes the terminal the collector is
     * cancelled, the socket closes, and herdr lets go of the screen — the pane keeps running.
     */
    /** The socket, once the flow is being collected. Writes before that are dropped. */
    @Volatile
    private var socket: WebSocket? = null

    fun events(): Flow<Event> = callbackFlow {
        val opened = client.terminalSocket(
            paneId = paneId,
            host = hostId,
            listener = object : WebSocketListener() {
                override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                    trySend(Event.Bytes(bytes.toByteArray()))
                }

                override fun onMessage(webSocket: WebSocket, text: String) {
                    // The only text frame the bridge sends is the pane's geometry. Anything else is
                    // herdr explaining itself (its CLI writes notices on this stream too) and is not
                    // an event this screen has a use for.
                    val shape = shapeOf(text) ?: return
                    trySend(shape)
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                    trySend(Event.Ended(reason.ifBlank { "closed" }))
                    close()
                }

                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    trySend(Event.Ended(t.message ?: "the computer stopped answering"))
                    close()
                }
            },
        )
        socket = opened
        awaitClose {
            socket = null
            opened.close(1000, "the screen was closed")
        }
    }

    /** One keystroke, as the bridge's wire takes it: raw bytes in a binary frame. */
    fun send(bytes: ByteArray) {
        if (bytes.isEmpty()) return
        socket?.send(ByteString.of(*bytes))
    }

    companion object {
        /**
         * Reads the bridge's geometry message, or null when this text frame is something else.
         *
         * Strict on purpose, including on types: the bridge encodes these as numbers, and a decoder
         * that also accepted `"183"` would turn a bug in the bridge into a terminal quietly sized to a
         * number somebody typed into a JSON string.
         */
        fun shapeOf(text: String): Event.Shape? {
            val parsed = runCatching { Json.parseToJsonElement(text).jsonObject }.getOrNull() ?: return null
            if (parsed["type"]?.jsonPrimitive?.contentOrNull != "size") return null
            val cols = (parsed["cols"]?.jsonPrimitive ?: return null).let { if (it.isString) return null else it }
                .intOrNull ?: return null
            val rows = (parsed["rows"]?.jsonPrimitive ?: return null).let { if (it.isString) return null else it }
                .intOrNull ?: return null
            if (cols <= 0 || rows <= 0) return null
            return Event.Shape(cols, rows)
        }
    }
}
