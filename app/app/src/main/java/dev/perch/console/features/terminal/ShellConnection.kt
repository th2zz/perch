package dev.perch.console.features.terminal

import dev.perch.console.core.herdrclient.BridgeClient
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString

class ShellConnection(private val client: BridgeClient, private val fresh: Boolean = false) {

    /** What the shell's stream tells us. */
    sealed interface Event {
        /** Raw bytes — the replay first (if there is one), then live output. */
        data class Bytes(val payload: ByteArray) : Event

        /**
         * The stream stopped.
         *
         * [gone] is true when the *shell* is finished: the bridge said so, and reattaching would only
         * fetch the same sentence again. False when the connection was what broke — the phone lost the
         * network, the socket was killed in the background, or this client fell behind and was dropped on
         * purpose — in which case the shell is still running, and coming back shows it.
         */
        data class Ended(val reason: String, val gone: Boolean) : Event
    }

    @Volatile
    private var socket: WebSocket? = null

    /** Whether the bridge has already told us the shell itself stopped. */
    @Volatile
    private var finished = false

    fun events(): Flow<Event> = callbackFlow {
        val opened = client.shellSocket(
            new = fresh,
            listener = object : WebSocketListener() {
                override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                    trySend(Event.Bytes(bytes.toByteArray()))
                }

                override fun onMessage(webSocket: WebSocket, text: String) {
                    val ended = endedOf(text) ?: return
                    finished = true
                    trySend(ended)
                }

                override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                    // The end frame, when there is one, has already been sent as a message by now; this
                    // only reports the socket. Saying "gone" twice would start two new shells.
                    if (!finished) {
                        trySend(Event.Ended(reason.ifBlank { "the computer stopped answering" }, gone = false))
                    }
                    close()
                }

                override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                    if (!finished) {
                        trySend(Event.Ended(t.message ?: "the computer stopped answering", gone = false))
                    }
                    close()
                }
            },
        )
        socket = opened
        awaitClose {
            socket = null
            // Letting go, not ending it: the shell keeps running on the computer, which is the whole point
            // of the session living there.
            opened.close(1000, "the terminal was closed")
        }
    }

    /** One keystroke, or a paste: raw bytes in a binary frame. */
    fun send(bytes: ByteArray) {
        if (bytes.isEmpty()) return
        socket?.send(ByteString.of(*bytes))
    }

    /** Tells the shell how big the phone's screen is. */
    fun resize(cols: Int, rows: Int) {
        if (cols <= 0 || rows <= 0) return
        socket?.send("""{"type":"resize","cols":$cols,"rows":$rows}""")
    }

    /** Ends this shell and starts another one. The socket ends with it; the screen reconnects. */
    fun restart() {
        socket?.send("""{"type":"restart"}""")
    }

    /** Ends the shell because the user asked for that. */
    fun closeShell() {
        socket?.send("""{"type":"close"}""")
    }

    companion object {
        /**
         * Reads the bridge's `ended` message, or null when this text frame is something else.
         *
         * Strict, like the pane stream's geometry reader: a text frame that is not one of ours is
         * something herdr or the bridge is saying, and turning it into an event would put words on the
         * screen that nobody wrote for the user. And a text frame *is* how the bridge says the shell is
         * finished, so this is also the only place that knows the difference between "come back" and
         * "there is nothing to come back to".
         */
        fun endedOf(text: String): Event.Ended? {
            val parsed = runCatching { Json.parseToJsonElement(text).jsonObject }.getOrNull() ?: return null
            if (parsed["type"]?.jsonPrimitive?.contentOrNull != "ended") return null
            val reason = (parsed["reason"] as? JsonPrimitive)?.contentOrNull ?: ""
            return Event.Ended(
                reason = reason.ifBlank { "the shell exited" },
                gone = true,
            )
        }
    }
}
