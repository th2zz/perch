package dev.perch.console

import dev.perch.console.core.live.BridgeEventTypes
import java.io.File
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The app's half of the event-stream contract.
 *
 * The bridge writes `contracts/bridge-events.json` and this
 * test reads it, so the two lists cannot drift apart in silence. That matters more
 * here than anywhere else on the wire: the app's handler has no fallback branch,
 * so an unrecognised type is ignored outright, and a rename on the bridge would
 * stop the phone being told that an agent is waiting — while every other test on
 * both sides stayed green.
 *
 * A plain JVM test: the thing being checked is a list of strings and a predicate
 * over them, not a screen or a connection.
 */
class BridgeEventsContractTest {

    @Serializable
    private data class EventsFixture(
        val why: String = "",
        val types: List<String> = emptyList(),
    )

    private val json = Json { ignoreUnknownKeys = true }

    private fun fixture(): EventsFixture = json.decodeFromString(
        ContractFixtures.text(
            "contracts/bridge-events.json",
            "PERCH_UPDATE_EVENTS_FIXTURE=1 go test ./internal/core/ -run EventTypes",
        ),
    )

    @Test
    fun theTypesTheAppKnowsAreTheTypesTheBridgeSends() {
        val types = fixture().types
        assertTrue("the shared event contract is empty", types.isNotEmpty())

        assertEquals(
            "the app's list of event types and the bridge's have drifted apart; " +
                "an unrecognised type is ignored, so this is how the phone goes quiet",
            types.sorted(),
            BridgeEventTypes.all.sorted(),
        )
    }

    @Test
    fun theHeartbeatIsTheOnlyThingThatDoesNotCauseARead() {
        val types = fixture().types

        // Everything the bridge sends, except the keepalive, means the queue may
        // have moved and is a reason to re-read. If a new type ever arrives and is
        // not classified here, this fails rather than being ignored at runtime.
        assertEquals(
            "every event type except the heartbeat should be a reason to re-read",
            (types - BridgeEventTypes.HEARTBEAT).sorted(),
            BridgeEventTypes.queueMoving.sorted(),
        )
        assertFalse(
            "the heartbeat must not trigger a re-read: it would make a quiet connection " +
                "cost a request every thirty seconds",
            BridgeEventTypes.movesTheQueue(BridgeEventTypes.HEARTBEAT),
        )
        for (type in types - BridgeEventTypes.HEARTBEAT) {
            assertTrue("$type should make the app re-read", BridgeEventTypes.movesTheQueue(type))
        }
        // And an unknown type — what an older app sees when a newer bridge adds one
        // — must not be treated as a reason to read: the app cannot know what it
        // meant, and guessing "go and look" on an unrecognised name is how a chatty
        // event turns into a request storm (the loop this codebase has fixed once).
        assertFalse(BridgeEventTypes.movesTheQueue("something_new"))
    }
}
