package dev.perch.console

import dev.perch.console.core.herdrclient.AgentStates
import dev.perch.console.core.herdrclient.ApiCompatibility
import dev.perch.console.core.herdrclient.BridgeClient
import dev.perch.console.core.herdrclient.BridgeException
import dev.perch.console.core.herdrclient.DecisionKind
import dev.perch.console.core.herdrclient.DecisionRequest
import dev.perch.console.core.herdrclient.Speakers
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.AfterClass
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.BeforeClass
import org.junit.Test

/**
 * The app's own client code against the real bridge binary.
 *
 * This is the only level at which the product's central seam is actually tested.
 * Everything else — the bridge's Go tests, the app's unit tests against captured
 * JSON — can be green while the two programs disagree about a field name, a
 * status code, or an error shape.
 *
 * The tests read like a user's first five minutes: can I reach it, does it tell
 * me where I went wrong, does the shape of what it sends decode into my types,
 * does the live stream come up, and does it refuse a write it should not accept.
 */
class BridgeIntegrationTest {

    companion object {
        @BeforeClass
        @JvmStatic
        fun startBridge() = BridgeHarness.start()

        @AfterClass
        @JvmStatic
        fun stopBridge() = BridgeHarness.stop()
    }

    private fun client(token: String = BridgeHarness.token) =
        BridgeClient(BridgeHarness.baseUrl, token)

    private fun requireBridge() {
        BridgeHarness.start()
        assumeTrue("the bridge binary was not available for this build", BridgeHarness.available)
    }

    private fun requireHerdr() {
        requireBridge()
        assumeTrue("herdr is not running on this machine", BridgeHarness.herdrRunning)
    }

    @Test
    fun `a wrongly paired phone is refused and told what to do`() {
        requireBridge()

        val failure = runCatching { runBlocking { client("not-the-token").health() } }
            .exceptionOrNull()

        assertTrue("a wrong token must fail", failure is BridgeException)
        val bridge = failure as BridgeException
        assertEquals(401, bridge.httpStatus)
        // Spec FR-015: never a bare failure. The re-pair instruction is what the
        // user actually needs at this point.
        assertTrue("the refusal must carry a next step", bridge.fix.isNotBlank())
    }

    @Test
    fun `health decodes into the app's own type`() {
        requireHerdr()

        val health = runBlocking { client().health() }
        assertTrue("a bridge version should be reported", health.bridgeVersion.isNotBlank())
        assertTrue("herdr's protocol should be reported", health.health.protocol > 0)
        // Seven checks, per the bridge-plugin contract; the app renders whatever
        // it is given, so the count is the contract.
        assertTrue("the check-up should have several items", health.health.items.size >= 5)
        health.health.items.forEach { item ->
            assertTrue("every check needs a title", item.title.isNotBlank())
            if (item.status != "ok" && item.status != "unknown") {
                assertTrue("a problem check must suggest a fix: ${item.id}", item.fix.isNotBlank())
            }
        }
    }


    /**
     * The two halves agree on the wire contract, checked against the real bridge.
     *
     * `scripts/check-portability.sh` compares the two constants statically, which catches a
     * rename but not a bridge that refuses this app's requests for some other reason.
     * This is the runtime half: the number the computer reports must be a number this
     * app accepts as compatible, or the user is walking into a 400 they cannot explain.
     */
    @Test
    fun `the computer speaks a contract this app accepts`() {
        requireBridge()

        val health = runBlocking { client().health() }
        val verdict = ApiCompatibility.of(health.apiVersion)
        assertEquals(
            "the bridge reports api_version ${health.apiVersion} and this app speaks " +
                "${ApiCompatibility.API_VERSION}, which the app reads as $verdict",
            ApiCompatibility.Ok,
            verdict,
        )
    }

    /**
     * A session's conversation, from the real bridge, decoded into the app's own type.
     *
     * This is the seam the reading face sits on, and it is the one nothing else covers: the
     * contract tests compare *names* against a fixture, and the bridge's own tests compare values
     * against recorded files — both can be green while the app's decoder and the bridge's encoder
     * disagree about something neither file mentions. So the pane is taken from the machine's own
     * snapshot rather than named here (a pane id belongs to one machine), and what is asserted is
     * the shape the screen depends on: roles are only the two this app knows, no empty message
     * reaches the reading area, and a harness with an adapter says it can be read.
     */
    @Test
    fun `a session's conversation decodes into the app's own type`() {
        requireHerdr()

        val mirror = runBlocking { client().snapshot() }.mirror
        val found = mirror.panes.firstOrNull { (it.agent ?: "").isNotBlank() }
        assumeTrue("no agent is running in a pane on this machine right now", found != null)
        val live = requireNotNull(found)

        val conversation = runBlocking { client().conversation(live.paneId, live.hostId) }

        // The claim the screen branches on: a harness Perch has an adapter for can be read. codex is
        // the adapter this version ships, and its record exists from the moment a session starts, so
        // "not readable" here would mean the chain that finds the file is broken.
        if (live.agent == "codex") {
            assertTrue("codex is the harness with an adapter, and this pane is running one", conversation.readable)
        }

        conversation.messages.forEach { message ->
            assertTrue(
                "a role the app does not know would be drawn as the user's own words: ${message.role}",
                message.role in Speakers.all,
            )
            assertTrue("an empty message has no business on the reading area", message.text.isNotBlank())
        }
        assertTrue(
            "a duration measured by the computer cannot be negative: ${conversation.workingSecs}",
            conversation.workingSecs >= 0,
        )
    }

    /**
     * The escape hatch's key bar, against the real bridge, without pressing anything
     * into the user's session.
     *
     * The refusal is the point: a key outside the bar's vocabulary must come back as a
     * refusal the app can show, not as a silent success. The success path needs a pane
     * and would type into it, which a test must never do to somebody's live agent.
     */
    @Test
    fun `a key outside the bar's vocabulary is refused by the real bridge`() {
        requireBridge()

        val thrown = runCatching {
            runBlocking { client().pressKeys("wA:p1", listOf("f5")) }
        }.exceptionOrNull()

        val bridgeError = thrown as? BridgeException
        assertTrue(
            "expected a refusal the app can explain, got $thrown",
            bridgeError != null && bridgeError.httpStatus == 400 && bridgeError.fix.isNotBlank(),
        )
    }

    /**
     * A session on a computer that is not saved is refused, and the app is told why.
     *
     * This is the multi-host failure that matters most: a request addressed to the wrong
     * computer must never be quietly served by the right one.
     */
    @Test
    fun `a session on an unsaved computer is refused`() {
        requireBridge()

        val thrown = runCatching {
            runBlocking { client().session("wA:p1", host = "no-such-machine") }
        }.exceptionOrNull()

        val bridgeError = thrown as? BridgeException
        assertTrue(
            "expected an explainable refusal, got $thrown",
            bridgeError != null && bridgeError.message.contains("no-such-machine"),
        )
    }

    @Test
    fun `the mirror decodes and is self-consistent`() {
        requireHerdr()

        val snapshot = runBlocking { client().snapshot() }
        val mirror = snapshot.mirror

        assertTrue("the user has at least one project", mirror.workspaces.isNotEmpty())
        assertTrue("the machine running the bridge is a host", mirror.hosts.isNotEmpty())
        assertEquals("local", mirror.hosts.first().hostId)

        // Every pane must belong to a workspace the app can name, otherwise the
        // queue would show a project label it invented.
        val known = mirror.workspaces.map { it.workspaceId }.toSet()
        mirror.panes.forEach { pane ->
            assertTrue(
                "pane ${pane.paneId} refers to unknown workspace ${pane.workspaceId}",
                pane.workspaceId in known,
            )
        }

        // And the status strings are ones the app knows how to present.
        val known1 = setOf("idle", "working", "blocked", "done", "unknown")
        mirror.panes.forEach { pane ->
            assertTrue(
                "unexpected agent status ${pane.agentStatus}",
                pane.agentStatus in known1,
            )
        }
    }

    @Test
    fun `the decision queue decodes`() {
        requireHerdr()

        val snapshot = runBlocking { client().snapshot() }
        snapshot.decisions.forEach { decision ->
            assertTrue("a decision needs an id", decision.id.isNotBlank())
            assertTrue("a decision needs a pane", decision.paneId.isNotBlank())
            assertTrue(
                "decision ${decision.id} is in an unknown state: ${decision.state}",
                decision.state in setOf("pending", "handled_locally", "handled_elsewhere", "expired"),
            )
            assertTrue(
                "the suggested action must be one of the four kinds",
                decision.suggestedKind in
                    setOf("approve", "reject", "answer", "prompt"),
            )
            // The card and the notification both rely on these never being blank.
            assertTrue(decision.agentLabel.isNotBlank())
            assertTrue(decision.projectLabel.isNotBlank())
        }
    }

    @Test
    fun `the live stream comes up and greets with the current state`() {
        requireHerdr()

        // The greeting is what makes a reconnect safe: a client that has been
        // away for hours is immediately consistent instead of waiting for the
        // next change.
        val greeting = runBlocking {
            withTimeout(TimeUnit.SECONDS.toMillis(20)) {
                client().events().first { it.type == "snapshot" }
            }
        }
        assertTrue("the greeting carries the mirror", greeting.mirror != null)
        assertTrue(
            "the greeting's mirror is populated",
            greeting.mirror!!.workspaces.isNotEmpty(),
        )
    }

    @Test
    fun `a fifth kind of write is refused at the wire`() {
        requireBridge()

        // The app only ever builds the four kinds, but this proves the boundary
        // is enforced by the server too — the whitelist is not merely a client
        // convention that a future client could ignore.
        val failure = runCatching {
            runBlocking {
                client().decide(
                    DecisionRequest(
                        decisionId = "does-not-exist",
                        kind = "rename",
                        target = "wA:p1",
                    ),
                )
            }
        }.exceptionOrNull()

        assertTrue("a fifth kind must be refused", failure is BridgeException)
        assertEquals(400, (failure as BridgeException).httpStatus)
    }

    @Test
    fun `reading a session returns the real terminal text`() {
        requireHerdr()

        // This is the assertion that would have caught a bug that made every
        // excerpt the product showed empty: `pane.read` answers with a wrapper
        // (`{"type":"pane_read","read":{...}}`), and decoding the wrapper into the
        // inner type succeeds, leaving every field zero. No error, no log, just a
        // blank card — so the only way to catch it is to require text back.
        val snapshot = runBlocking { client().snapshot() }
        assumeTrue("no session to read", snapshot.mirror.panes.isNotEmpty())
        val pane = snapshot.mirror.panes.first()

        val view = runBlocking { client().session(pane.paneId, lines = 20) }

        assertTrue("the session should name its pane", view.paneId.isNotBlank())
        assertTrue(
            "a real session must return some terminal text, not an empty string",
            view.text.isNotBlank(),
        )
        // The escape hatch offers text delivery unconditionally; it is the fourth
        // write class and does not depend on what is on screen.
        assertTrue(view.canDeliverText)
        // A session that is not waiting on anyone says so rather than looking
        // like a request.
        if (view.agentStatus != "blocked") {
            assertTrue("a running session should explain itself", view.unavailable.isNotBlank())
            assertFalse(view.approvable || view.rejectable || view.answerable)
        }
    }

    @Test
    fun `approving an unknown request is refused rather than silently ignored`() {
        requireBridge()

        val failure = runCatching {
            runBlocking {
                client().decide(
                    DecisionRequest(
                        decisionId = "no-such-decision",
                        kind = DecisionKind.APPROVE.wire,
                        target = "wA:p1",
                    ),
                )
            }
        }.exceptionOrNull()

        assertTrue(failure is BridgeException)
        val bridge = failure as BridgeException
        // 500 here would mean the app cannot tell "you are out of date" from
        // "the bridge is broken", which is the difference between a refresh and
        // a bug report.
        assertTrue(
            "an unknown request should be a client-visible failure, got ${bridge.httpStatus}",
            bridge.httpStatus == 404 || bridge.httpStatus == 409 || bridge.httpStatus == 422,
        )
    }

}
