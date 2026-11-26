package dev.perch.console

import dev.perch.console.core.herdrclient.AgentStates
import dev.perch.console.core.herdrclient.BridgeClient
import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.DecisionRequest
import dev.perch.console.core.herdrclient.SnapshotResponse
import dev.perch.console.core.herdrclient.StreamEvent
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * Protocol tests against payloads captured from the running bridge.
 *
 * The JSON below is not invented: it is the shape the bridge actually sent over
 * its socket on 2026-10-13, trimmed to one workspace and one pane. A test built
 * from a hand-written fixture would only prove the fixture is consistent with
 * itself.
 */
class BridgeProtocolTest {

    private val json = Json {
        ignoreUnknownKeys = true
        explicitNulls = false
        encodeDefaults = true
    }

    private val capturedSnapshot = """
    {
      "mirror": {
        "hosts": [
          {"host_id": "local", "label": "dev-laptop", "kind": "local", "status": "reachable", "enabled": true}
        ],
        "herdr_version": "0.9.1",
        "protocol": 22,
        "taken_at": "2026-10-13T07:57:13.211298964+09:00",
        "workspaces": [
          {"workspace_id": "w2", "number": 1, "label": "backend", "focused": false,
           "pane_count": 2, "tab_count": 2, "active_tab_id": "w2:t1", "agent_status": "idle"}
        ],
        "tabs": [],
        "panes": [
          {"pane_id": "w2:p1", "workspace_id": "w2", "tab_id": "w2:t1", "focused": false,
           "agent": "codex", "display_agent": null, "agent_status": "idle",
           "cwd": "/home/user/workspace", "title": null,
           "terminal_title": "Check build logs and test output | workspace"}
        ],
        "agents": []
      },
      "decisions": [
        {"id": "wA:p3@1791845850.1", "host_id": "local", "pane_id": "wA:p3", "workspace_id": "wA",
         "workspace_label": "mobile agent", "tab_id": "", "agent_kind": "probe", "display_agent": "",
         "title": "", "rule": "", "prompt_text": "",
         "suggested_kind": "approve", "approvable": false, "rejectable": false,
         "unmappable_reason": "herdr reported a blocked agent but did not name a detection rule",
         "created_at": "2026-10-13T07:57:30.289904513+09:00",
         "updated_at": "2026-10-13T07:57:30.495940405+09:00",
         "state": "pending"}
      ]
    }
    """.trimIndent()

    @Test
    fun decodesWhatTheBridgeActuallySends() {
        val snapshot = json.decodeFromString<SnapshotResponse>(capturedSnapshot)

        assertEquals("0.9.1", snapshot.mirror.herdrVersion)
        assertEquals(22, snapshot.mirror.protocol)
        assertEquals(1, snapshot.mirror.workspaces.size)
        assertEquals("backend", snapshot.mirror.workspaces[0].label)
        assertEquals(1, snapshot.mirror.panes.size)

        // CJK survives the round trip. This is the product's first-order
        // requirement, so it is asserted rather than assumed.
        assertEquals(
            "Check build logs and test output | workspace",
            snapshot.mirror.panes[0].terminalTitle,
        )

        assertEquals(1, snapshot.decisions.size)
        val decision = snapshot.decisions[0]
        assertEquals("wA:p3", decision.paneId)
        assertEquals("mobile agent", decision.projectLabel)
        // agent_kind is known and display_agent is empty, so the label falls back.
        assertEquals("probe", decision.agentLabel)
        assertFalse(decision.approvable)
        assertTrue(decision.unmappableReason!!.isNotBlank())
    }

    @Test
    fun unknownFieldsFromANewerBridgeAreIgnored() {
        // `updated_at` above is already an example: the Kotlin model does not
        // declare it and decoding still succeeds. This is what lets the bridge
        // gain a field without shipping an app update in lockstep.
        val withExtra = capturedSnapshot.replace(
            "\"state\": \"pending\"",
            "\"state\": \"pending\", \"something_new\": {\"a\": 1}",
        )
        val snapshot = json.decodeFromString<SnapshotResponse>(withExtra)
        assertEquals("pending", snapshot.decisions[0].state)
    }

    @Test
    fun anAgentStateWeHaveNeverSeenDoesNotBreakTheApp() {
        // herdr may add a state in a future release. The app must keep working and
        // must not present it as a decision or as completion.
        val future = capturedSnapshot.replace("\"agent_status\": \"idle\"", "\"agent_status\": \"thinking\"")
        val snapshot = json.decodeFromString<SnapshotResponse>(future)
        assertEquals("thinking", snapshot.mirror.panes[0].agentStatus)
        assertFalse(AgentStates.needsDecision(snapshot.mirror.panes[0].agentStatus))
    }

    @Test
    fun onlyBlockedNeedsADecision() {
        assertTrue(AgentStates.needsDecision("blocked"))
        for (other in listOf("idle", "working", "done", "unknown", "thinking")) {
            assertFalse(other, AgentStates.needsDecision(other))
        }
    }

    @Test
    fun decisionRequestCarriesOnlyTheFieldsTheBridgeAccepts() {
        val encoded = json.encodeToString(
            DecisionRequest(decisionId = "d1", kind = "approve", target = "wA:p1", text = null),
        )
        // The bridge rejects unknown fields outright, so the app must not send
        // anything extra. This assertion is the app-side half of that contract.
        assertFalse(encoded.contains("text"))
        assertTrue(encoded.contains("\"kind\":\"approve\""))
        // `host` is the deliberate fifth field (spec FR-016): a pane id is only
        // unique within one computer, so the request has to say which one. It is
        // always present — empty meaning this computer — because a field that is
        // sometimes there and sometimes not is how one of the two implementations
        // ends up disagreeing with the other.
        assertEquals(
            "{\"decision_id\":\"d1\",\"kind\":\"approve\",\"target\":\"wA:p1\",\"host\":\"\"}",
            encoded,
        )
        assertEquals(
            "{\"decision_id\":\"d1\",\"kind\":\"approve\",\"target\":\"wA:p1\"," +
                "\"host\":\"workbox\"}",
            json.encodeToString(
                DecisionRequest(
                    decisionId = "d1",
                    kind = "approve",
                    target = "wA:p1",
                    host = "workbox",
                ),
            ),
        )
    }

    @Test
    fun pairingAcceptsWhatAUserWouldActuallyType() {
        // Tailscale address, as shown by `perch-bridge pair`.
        assertEquals("100.64.0.1", BridgeClient.parseAddress("100.64.0.1:45999")?.host)
        assertEquals(45999, BridgeClient.parseAddress("100.64.0.1:45999")?.port)
        // With a scheme, and with surrounding whitespace from a copy-paste.
        assertEquals("https", BridgeClient.parseAddress(" https://my-box:45999 ")?.scheme)
        // Hostname without a port still parses; the port is the user's to fix.
        assertEquals("my-box", BridgeClient.parseAddress("my-box")?.host)

        assertNull(BridgeClient.parseAddress(""))
        assertNull(BridgeClient.parseAddress("   "))
    }

    @Test
    fun streamEventsDecodeIncludingOnesWeDoNotAct() {
        val heartbeat = json.decodeFromString<StreamEvent>("""{"type":"heartbeat"}""")
        assertEquals("heartbeat", heartbeat.type)

        val closed = json.decodeFromString<StreamEvent>("""{"type":"closed","reason":"socket hang up"}""")
        assertEquals("closed", closed.type)
        assertEquals("socket hang up", closed.reason)
    }

    @Test
    fun decisionLabelsNeverShowAnEmptyCard() {
        // Every one of these would render as " in " without the fallbacks.
        val bare = Decision()
        assertEquals("agent", bare.agentLabel)
        assertEquals("a project", bare.projectLabel)

        val kindOnly = Decision(agentKind = "codex")
        assertEquals("codex", kindOnly.agentLabel)
    }
}
