package dev.perch.console.core.herdrclient

import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.withContext
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener

/**
 * An error the user can act on.
 *
 * The bridge promises a reason and a next step with every failure (spec FR-015),
 * so the app carries both rather than flattening them into one string.
 */
class BridgeException(
    override val message: String,
    val fix: String = "",
    val httpStatus: Int = 0,
    cause: Throwable? = null,
) : IOException(message, cause)

/**
 * Talks to the bridge running on the user's own computer.
 *
 * Every call is a request to the computer; nothing is cached here. The cache
 * lives in [dev.perch.console.core.cache.MirrorCache] and is explicitly
 * disposable, because the computer remains the source of truth
 * (constitution I).
 */
class BridgeClient(
    private val baseUrl: HttpUrl,
    private val token: String,
    private val http: OkHttpClient = defaultHttp(),
) {
    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }
    private val jsonMedia = "application/json; charset=utf-8".toMediaType()

    /**
     * Creation waits for herdr to start an agent, which can outlast the screen's own call budget.
     *
     * The bridge allows 90 seconds for one creation. The shared client's 25-second call timeout exists
     * for the notification receiver, where the platform kills a background process after about ten
     * seconds; applying that budget here would abandon a start that is still making progress and leave
     * the person looking at "nothing happened". The bridge cancels and cleans up its own work when the
     * phone really is gone, so a patient socket is the honest one for these two calls.
     */
    private val createHttp: OkHttpClient by lazy {
        http.newBuilder()
            .callTimeout(120, TimeUnit.SECONDS)
            .readTimeout(120, TimeUnit.SECONDS)
            .writeTimeout(30, TimeUnit.SECONDS)
            .build()
    }

    /** Full state: what the computer sees right now, plus the decision queue. */
    suspend fun snapshot(): SnapshotResponse =
        json.decodeFromString(get(Endpoints.SNAPSHOT))

    suspend fun health(): HealthResponse =
        json.decodeFromString(get(Endpoints.HEALTH))

    suspend fun value(): ValueResponse =
        json.decodeFromString(get(Endpoints.VALUE))

    /**
     * Reads one session, for the escape hatch. Read-only.
     *
     * `host` is the computer the session is on; empty means the one Perch runs on.
     */
    suspend fun session(paneId: String, lines: Int = 160, host: String = ""): SessionView =
        json.decodeFromString(
            get(Endpoints.SESSION) { url ->
                // Query parameters go through the builder, never into the path
                // string. Appending "?x=y" to the path would percent-encode the
                // question mark and the server would see one long path — which is
                // exactly the 404 this code used to return.
                url.addQueryParameter("pane_id", paneId)
                url.addQueryParameter("lines", lines.toString())
                if (host.isNotBlank()) url.addQueryParameter("host", host)
            },
        )

    /**
     * Reads what the harness itself knows about a session: model, spending, goal.
     *
     * On demand, because the computer answers it by reading a file that can be tens of
     * megabytes — never on a poll.
     */
    suspend fun usage(paneId: String, host: String = ""): UsageFacts =
        json.decodeFromString(
            get(Endpoints.SESSION_USAGE) { url ->
                url.addQueryParameter("pane_id", paneId)
                if (host.isNotBlank()) url.addQueryParameter("host", host)
            },
        )

    /**
     * Reads a session's conversation from the agent's own records.
     *
     * This is the reading face's source, and unlike [usage] it *is* polled — an answer has to arrive
     * as it is written. That is affordable because the bridge tails the record rather than parsing
     * it again: a session on this machine was 56 MB after four days, and re-reading it every 0.8
     * seconds is the one thing this call must not become.
     */
    suspend fun conversation(paneId: String, host: String = ""): Transcript =
        json.decodeFromString(
            get(Endpoints.SESSION_CONVERSATION) { url ->
                url.addQueryParameter("pane_id", paneId)
                if (host.isNotBlank()) url.addQueryParameter("host", host)
            },
        )

    /**
     * Submits one decision.
     *
     * The body is exactly the four-field shape the bridge accepts; the app cannot
     * ask for anything else, which is how constitution IV is enforced from this
     * side too.
     */
    suspend fun decide(request: DecisionRequest): DecideResponse {
        val body = json.encodeToString(request).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.DECISIONS).post(body).build()
        return withContext(Dispatchers.IO) {
            http.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
                json.decodeFromString(response.body.string())
            }
        }
    }

    /**
     * Presses keys on a session, from the escape hatch's key bar.
     *
     * This is not a decision and deliberately does not go through
     * [dev.perch.console.features.queue.DecisionExecutor]: the four decision
     * classes are the app acting for the user, and they are closed. This is the
     * user operating their own terminal, which is why the endpoint is only
     * reachable from an open session and why the vocabulary is fixed on both
     * sides (see [dev.perch.console.features.terminal.TerminalKeys]).
     */
    suspend fun pressKeys(paneId: String, keys: List<String>, host: String = ""): KeysResponse {
        val body = json.encodeToString(KeysRequest(paneId, keys, host)).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.SESSION_KEYS).post(body).build()
        return withContext(Dispatchers.IO) {
            http.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
                json.decodeFromString(response.body.string())
            }
        }
    }

    /**
     * Names a session or a project, and reports the herdr command that did it.
     *
     * The two targets travel as one endpoint with a `target` field, because the product's question is "what
     * can this phone change?" and the answer — a name — is one thing. [ManageTarget] keeps the strings from
     * being invented at a call site.
     */
    suspend fun rename(
        target: ManageTarget,
        id: String,
        label: String,
        host: String = "",
    ): ManageResult {
        val body = json.encodeToString(RenameRequest(target.wire, id, label, host)).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.RENAME).post(body).build()
        return withContext(Dispatchers.IO) {
            http.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
                json.decodeFromString(response.body.string())
            }
        }
    }

    /**
     * Ends a session, or a whole project.
     *
     * [confirmed] is required for a project and meaningless for a session — the bridge refuses an
     * unconfirmed project close, so a bug in a screen cannot take out every agent somebody had running.
     * Nothing about that refusal is a formality: it is the one action in this app that cannot be undone.
     */
    suspend fun close(
        target: ManageTarget,
        id: String,
        host: String = "",
        confirmed: Boolean = false,
    ): ManageResult {
        val body = json.encodeToString(CloseRequest(target.wire, id, host, confirmed)).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.CLOSE).post(body).build()
        return withContext(Dispatchers.IO) {
            http.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
                json.decodeFromString(response.body.string())
            }
        }
    }

    suspend fun harnesses(host: String = ""): HarnessesResponse {
        val body = get(Endpoints.HARNESSES) { if (host.isNotBlank()) it.addQueryParameter("host", host) }
        return json.decodeFromString(body)
    }

    /**
     * Lists the folders inside one directory on the computer.
     *
     * A read, and the source of the picker's rows: an empty [path] asks for the home folder, which is
     * how the picker learns where "home" is without a second endpoint. Every step the person takes —
     * up, home, into a row — is another one of these calls, because the computer is the only thing
     * that knows what is still there.
     */
    suspend fun folders(path: String = "", host: String = ""): FoldersResponse {
        val body = get(Endpoints.FOLDERS) { url ->
            if (path.isNotBlank()) url.addQueryParameter("path", path)
            if (host.isNotBlank()) url.addQueryParameter("host", host)
        }
        return json.decodeFromString(body)
    }

    suspend fun createProject(
        label: String,
        cwd: String,
        kind: String,
        name: String = "",
        args: List<String> = emptyList(),
        host: String = "",
    ): CreateResult {
        val request = CreateProjectRequest(label, cwd, kind, name, args, host)
        val body = json.encodeToString(request).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.PROJECTS).post(body).build()
        return withContext(Dispatchers.IO) {
            createHttp.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
                json.decodeFromString(response.body.string())
            }
        }
    }

    suspend fun createSession(
        workspaceId: String,
        kind: String,
        name: String = "",
        args: List<String> = emptyList(),
        host: String = "",
    ): CreateResult {
        val request = CreateSessionRequest(workspaceId, kind, name, args, host)
        val body = json.encodeToString(request).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.SESSIONS).post(body).build()
        return withContext(Dispatchers.IO) {
            createHttp.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
                json.decodeFromString(response.body.string())
            }
        }
    }

    /** Hands the notification token to the computer so it can reach this phone. */
    /**
     * Tells the computer what happened on the terminal screen.
     *
     * Best effort and never awaited: this is a log line for the host, not a state change, and a terminal
     * must not wait on a report about itself. It exists because "the terminal is black on my phone" has
     * no host-side trace otherwise — the bridge sees an attach, which is true and useless (2026-10-14).
     */
    suspend fun reportTerminal(paneId: String, event: String, detail: String) {
        val body = json.encodeToString(
            TerminalReport(paneId = paneId, event = event, detail = detail),
        ).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.TERMINAL_REPORT).post(body).build()
        withContext(Dispatchers.IO) {
            runCatching { http.newCall(httpRequest).execute().use { it.body.close() } }
        }
    }

    /**
     * Sends a picture of the phone's screen to the computer, because the user asked for it.
     *
     * Returns where it was saved, for the log line. Never called on a timer and never automatically: the
     * only thing that invokes this is a button in the terminal's header, pressed by the person holding the
     * phone. Two days of "it is black" was two days of nobody being able to *see* the screen — a pixel
     * question that only words had been answered with.
     */
    suspend fun reportScreenshot(paneId: String, pngBase64: String): String? {
        val body = json.encodeToString(
            ScreenshotReport(paneId = paneId, pngBase64 = pngBase64),
        ).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.SCREENSHOT).post(body).build()
        return withContext(Dispatchers.IO) {
            runCatching {
                http.newCall(httpRequest).execute().use { response ->
                    if (!response.isSuccessful) return@use null
                    runCatching {
                        json.decodeFromString<ScreenshotSaved>(response.body.string()).saved
                    }.getOrNull()
                }
            }.getOrNull()
        }
    }

    suspend fun registerPushToken(deviceToken: String) {
        val body = json.encodeToString(PushRegistration(deviceToken)).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.PUSH_REGISTER).post(body).build()
        withContext(Dispatchers.IO) {
            http.newCall(httpRequest).execute().use { response ->
                if (!response.isSuccessful) throw errorFrom(response)
            }
        }
    }

    /**
     * Reports how long this phone took to put a decision on screen.
     *
     * Best effort by design: it is a measurement, and a measurement that fails
     * must never disturb the thing being measured.
     */
    suspend fun reportNotificationShown(decisionId: String, shownAfterMs: Long) {
        val body = json.encodeToString(NotifiedReport(decisionId, shownAfterMs))
            .toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.NOTIFIED).post(body).build()
        withContext(Dispatchers.IO) {
            runCatching {
                http.newCall(httpRequest).execute().use { it.body.close() }
            }
        }
    }

    /**
     * Reports how long this phone took to become usable.
     *
     * Best effort, like the notification report, and for the same reason: it is a
     * measurement, and a measurement must never delay the thing it measures.
     */
    suspend fun reportColdStart(startupMillis: Long) {
        val body = json.encodeToString(StartupReport(startupMillis)).toRequestBody(jsonMedia)
        val httpRequest = authorized(Endpoints.STARTUP).post(body).build()
        withContext(Dispatchers.IO) {
            runCatching {
                http.newCall(httpRequest).execute().use { it.body.close() }
            }
        }
    }

    /**
     * Opens the terminal WebSocket for a pane.
     *
     * The URL is built the same way every other request is (see [authorized]), so authentication is
     * one mechanism rather than two, and the size travels in the query because the *host* terminal
     * must be created at the phone's shape — attaching at a made-up size and resizing afterwards makes
     * the screen repaint twice and the user sees the jump.
     */
    fun terminalSocket(
        paneId: String,
        host: String,
        listener: WebSocketListener,
    ): WebSocket {
        val request = authorized(Endpoints.TERMINAL) { url ->
            url.addQueryParameter("pane_id", paneId)
            // No size, and that is the point: attaching at the phone's shape reflows the program the
            // user is watching on their computer, and herdr does not put it back afterwards. The pane
            // keeps its geometry; the phone draws that geometry instead, which the bridge reports on
            // the stream as soon as it attaches.
            if (host.isNotBlank()) url.addQueryParameter("host", host)
        }.build()
        return http.newWebSocket(request, listener)
    }

    fun shellSocket(new: Boolean, listener: WebSocketListener): WebSocket {
        val request = authorized(Endpoints.SHELL) { url ->
            if (new) url.addQueryParameter("new", "1")
        }.build()
        return http.newWebSocket(request, listener)
    }

    /**
     * The live event stream.
     *
     * Reconnection is the caller's business: the flow emits a [StreamEvent] with
     * type "closed" when the socket drops, and the repository decides whether to
     * retry. That keeps the retry policy next to the UI state it affects instead
     * of hidden inside a transport.
     */
    fun events(): Flow<StreamEvent> = callbackFlow {
        val request = authorized(Endpoints.STREAM).build()
        val listener = object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, text: String) {
                val event = runCatching { json.decodeFromString<StreamEvent>(text) }.getOrNull()
                if (event != null) trySend(event)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                trySend(StreamEvent(type = "closed", reason = t.message))
                close()
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                trySend(StreamEvent(type = "closed", reason = reason))
                close()
            }
        }
        val socket = http.newWebSocket(request, listener)
        awaitClose { socket.cancel() }
    }

    // ------------------------------------------------------------ internals

    /**
     * Every path this app can ask the computer for.
     *
     * Collected in one place so that "how many ways can this app change the user's
     * computer?" is a property a test can hold on to. A future contributor adding,
     * say, a focus call would have to add it here, and the test beside this file
     * would fail and make them think about whether they are inventing a fifth kind
     * of write.
     */
    object Endpoints {
        const val HEALTH = "/v1/health"
        const val SNAPSHOT = "/v1/snapshot"
        const val DECISIONS = "/v1/decisions"
        const val VALUE = "/v1/value"
        const val SESSION = "/v1/session"
        const val SESSION_KEYS = "/v1/session/keys"
        const val SESSION_USAGE = "/v1/session/usage"
        const val SESSION_CONVERSATION = "/v1/session/conversation"
        const val TERMINAL = "/v1/session/terminal"
        const val SHELL = "/v1/terminal"
        const val RENAME = "/v1/rename"
        const val CLOSE = "/v1/close"
        const val HARNESSES = "/v1/harnesses"
        /** The working-folder picker's read. It creates nothing; it only lists what is there. */
        const val FOLDERS = "/v1/folders"
        const val PROJECTS = "/v1/projects"
        const val SESSIONS = "/v1/sessions"
        const val STREAM = "/v1/stream"
        const val PUSH_REGISTER = "/v1/push/register"
        const val NOTIFIED = "/v1/feedback/notified"
        const val STARTUP = "/v1/feedback/startup"
        const val TERMINAL_REPORT = "/v1/feedback/terminal"
        /** A picture of the phone's own screen, sent because the user pressed a button. */
        const val SCREENSHOT = "/v1/feedback/screenshot"

        val ALL = listOf(
            HEALTH, SNAPSHOT, DECISIONS, VALUE, SESSION, SESSION_KEYS, SESSION_USAGE,
            SESSION_CONVERSATION, TERMINAL, SHELL, RENAME, CLOSE, HARNESSES, PROJECTS, SESSIONS,
            STREAM, PUSH_REGISTER, NOTIFIED, STARTUP, TERMINAL_REPORT, SCREENSHOT, FOLDERS,
        )

        /**
         * Every path at which the app can change something on the user's computer.
         *
         * They are not of a kind, and the distinction is the product's whole safety story:
         *
         *  - [DECISIONS] is the app acting *for* the user: four closed classes, each provably
         *    equivalent to one herdr command, and a refusal rather than a guess when the mapping
         *    cannot be determined.
         *  - [SESSION] (POST) and [SESSION_KEYS] are the user acting *themselves* — text typed, and
         *    keys pressed, in a session they deliberately opened.
         *  - [TERMINAL] is the same thing taken to its conclusion (D34): a real terminal on the
         *    phone, so what travels is whatever the user types, byte for byte.
         *
         * This list was wrong until 2026-10-15 in both directions — two entries long, because
         * [SESSION_KEYS] lost its last caller when the real terminal arrived, and one short, because
         * [TERMINAL] was added and never listed here. A list like this is worth keeping only if it is
         * true, which is why [ApiSurfaceTest] now checks it against the declared constants rather than
         * against itself.
         */
        val WRITES = listOf(
            DECISIONS, SESSION, SESSION_KEYS, TERMINAL, SHELL, RENAME, CLOSE, PROJECTS, SESSIONS,
            SCREENSHOT,
        )

        /** The decisions door: the only path with the four write classes behind it. */
        const val ONLY_WRITE = DECISIONS
    }

    private fun authorized(
        path: String,
        query: (HttpUrl.Builder) -> Unit = {},
    ): Request.Builder {
        val url = baseUrl.newBuilder().addPathSegments(path.trimStart('/'))
        query(url)
        return Request.Builder()
            .url(url.build())
            .header("Authorization", "Bearer $token")
    }

    private suspend fun get(path: String, query: (HttpUrl.Builder) -> Unit = {}): String =
        withContext(Dispatchers.IO) {
            val request = authorized(path, query).get().build()
            try {
                readOnce(request)
            } catch (e: IOException) {
                val started = System.nanoTime()
                val millis = (System.nanoTime() - started) / 1_000_000
                // A failure that came back *quickly* is a connection that was gone (a carrier or VPN
                // dropping an idle flow): the next connection is a different one, and retrying it here is
                // cheap and invisible. A failure that took its whole timeout is a link that is asleep, and
                // hammering it again inside the same call would only make the user wait twice — that one
                // belongs to the caller, which can say "still opening" and try again with a pause.
                // Whatever went wrong, the connection it went wrong on is not to be trusted again — a read
                // timeout means the request went out and nothing came back, which is exactly what a socket
                // the network has forgotten looks like. (The first version of this only evicted after a
                // *fast* failure, so a slow one left the poisoned connection in the pool for the next
                // attempt to reuse.)
                http.connectionPool.evictAll()
                val text = readOnce(request)
                val kind = e::class.simpleName ?: "failure"
                // Reported *after* the retry succeeded, on the connection that works: a report about a
                // link that was asleep would go to sleep with it.
                reportRetry(path, "$kind after ${millis}ms, retry succeeded")
                return@withContext text
            }
        }

    /** Below this, a failure was a dead connection rather than a slow one. */
    private val fastFailureMillis = 3_000L

    /**
     * One line about a read that had to be retried, on the computer's log.
     *
     * Fire and forget, and deliberately narrow: a retry is either invisible (good) or evidence (useful),
     * and this is the only way to tell which without a phone in my hand.
     */
    private suspend fun reportRetry(path: String, detail: String) {
        runCatching { reportTerminal(paneId = path, event = "app", detail = "read retried — $detail") }
    }

    private fun readOnce(request: Request): String =
        http.newCall(request).execute().use { response ->
            if (!response.isSuccessful) throw errorFrom(response)
            response.body.string()
        }

    private fun errorFrom(response: Response): BridgeException {
        val text = runCatching { response.body.string() }.getOrDefault("")
        val parsed = runCatching { json.decodeFromString<ErrorBody>(text) }.getOrNull()
        return BridgeException(
            message = parsed?.error?.takeIf { it.isNotBlank() }
                ?: "the computer returned ${response.code}",
            fix = parsed?.fix.orEmpty(),
            httpStatus = response.code,
        )
    }

    companion object {
        /**
         * The HTTP client, with a total call budget chosen for the tightest caller.
         *
         * The tightest caller is not the screen. It is the Approve button on a
         * notification: that runs inside a broadcast receiver, and the platform
         * gives a background receiver roughly ten seconds before it is considered
         * unresponsive. If a call outlives that window the process can be killed
         * mid-flight, and the user's tap would then do nothing at all — no
         * approval, no queued reply, no message. Bounding every call to fit inside
         * that window is what guarantees the two outcomes we promise: it worked,
         * or it is saved for later.
         *
         * The generous reading is that this is far more time than the work needs:
         * every call goes to a machine on the user's own network, where the honest
         * answer to a ten-second wait is "your computer is not there".
         */
        fun defaultHttp(): OkHttpClient = OkHttpClient.Builder()
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(20, TimeUnit.SECONDS)
            .callTimeout(25, TimeUnit.SECONDS)
            .pingInterval(20, TimeUnit.SECONDS)
            .retryOnConnectionFailure(true)
            .connectionPool(okhttp3.ConnectionPool(2, 20, TimeUnit.SECONDS))
            .build()

        /**
         * Normalises whatever the user typed into a base URL.
         *
         * The pairing screen asks for one address and one token, so the app has to
         * accept "100.64.0.1:45999" without a scheme and without making the user
         * learn what a scheme is (spec SC-005).
         */
        fun parseAddress(raw: String): HttpUrl? {
            val trimmed = raw.trim()
            if (trimmed.isEmpty()) return null
            val withScheme = if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) {
                trimmed
            } else {
                "http://$trimmed"
            }
            return withScheme.toHttpUrlOrNull()
        }
    }
}
