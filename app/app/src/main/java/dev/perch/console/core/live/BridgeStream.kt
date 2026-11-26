package dev.perch.console.core.live

import dev.perch.console.core.herdrclient.BridgeClient
import dev.perch.console.core.herdrclient.StreamEvent
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * How the phone is currently doing at staying in touch with the computer.
 *
 * This is shown to the user, which is why it is a small honest set rather than a
 * boolean: "reconnecting" is a different situation from "you are not paired",
 * and conflating them would hide the case where the computer is asleep.
 */
enum class ConnectionStatus {
    /** No pairing configured. */
    Unpaired,

    /** First connection attempt in progress. */
    Connecting,

    /** The stream is up; what the user sees is current. */
    Live,

    /** The stream dropped and a retry is scheduled. */
    Retrying,
}

/**
 * The single live connection to the user's computer.
 *
 * Exactly one stream is kept for the whole app, and everybody — the console
 * screen and the notification service alike — reads from it. Two consequences
 * matter:
 *
 *  1. The computer sees one connection, not one per screen. The bridge is a
 *     small process on a machine that is also running agents, so the phone has
 *     no business opening a socket per view.
 *  2. There is one reconnection policy in one place. When the phone changes
 *     networks or the computer sleeps, the retry behaviour is decided here and
 *     reported to the user through [status], rather than being duplicated and
 *     subtly different in two places.
 *
 * The stream is a *hint* channel: an event means "something changed, re-read",
 * never "here is the new truth". A stream that has silently fallen behind is the
 * one failure mode that would make the app lie about what needs the user, so
 * nothing here is ever applied directly to the visible state.
 */
object BridgeStream {

    private const val FIRST_RETRY_MILLIS = 1_000L
    private const val MAX_RETRY_MILLIS = 30_000L

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private val _events = MutableSharedFlow<StreamEvent>(extraBufferCapacity = 64)
    val events: SharedFlow<StreamEvent> = _events.asSharedFlow()

    private val _status = MutableStateFlow(ConnectionStatus.Unpaired)
    val status: StateFlow<ConnectionStatus> = _status.asStateFlow()

    private var job: Job? = null

    /**
     * How the long waits end early when the network comes back.
     *
     * See [ReconnectPacer]: the backoff measures our attempts, not the outage, so
     * without this the worst case is a thirty-second wait that happens to be
     * spent with the network already working (SC-008 promises five seconds).
     */
    private val pacer = ReconnectPacer()

    /**
     * Says the network situation changed; stop waiting out the backoff.
     *
     * Called by [NetworkWatch] when a network appears. Public because the app's
     * own lifecycle is not the only thing that knows: the watching service holds
     * the connection while the app is closed.
     */
    fun retryNow() {
        pacer.nudge()
    }

    /**
     * Starts the connection and keeps it up until [stop] is called.
     *
     * [clientProvider] is called fresh on every attempt, so a pairing change (or
     * an unpair) takes effect on the next retry without restarting anything.
     */
    fun start(clientProvider: () -> BridgeClient?) {
        if (job?.isActive == true) return
        job = scope.launch {
            var backoff = FIRST_RETRY_MILLIS
            while (true) {
                val client = clientProvider()
                if (client == null) {
                    _status.value = ConnectionStatus.Unpaired
                    return@launch
                }
                _status.value =
                    if (backoff == FIRST_RETRY_MILLIS) ConnectionStatus.Connecting
                    else ConnectionStatus.Retrying
                try {
                    client.events().collect { event ->
                        if (event.type == "closed") return@collect
                        // Any traffic at all means the connection is healthy.
                        _status.value = ConnectionStatus.Live
                        backoff = FIRST_RETRY_MILLIS
                        _events.tryEmit(event)
                    }
                } catch (_: Exception) {
                    // Fall through: the backoff below is the whole error policy.
                }
                _status.value = ConnectionStatus.Retrying
                // A nudge means the reason for waiting is gone, so the next attempt
                // starts from the fast retry again rather than from where the
                // backoff had climbed to.
                backoff = if (pacer.wait(backoff)) {
                    FIRST_RETRY_MILLIS
                } else {
                    (backoff * 2).coerceAtMost(MAX_RETRY_MILLIS)
                }
            }
        }
    }

    /** Stops the connection; used on unpair and by the service on shutdown. */
    fun stop() {
        job?.cancel()
        job = null
        _status.value = ConnectionStatus.Unpaired
    }
}
