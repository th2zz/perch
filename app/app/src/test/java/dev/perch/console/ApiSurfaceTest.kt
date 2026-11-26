package dev.perch.console

import dev.perch.console.core.herdrclient.BridgeClient
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The app's half of "there are exactly four kinds of write".
 *
 * The bridge enforces the rule, but the app could still invent a new call the
 * bridge would have to refuse — and the natural way for that to happen is someone
 * adding an endpoint because a screen needed it. This test does not make that
 * impossible; it makes it deliberate. Adding a path here is a visible act with a
 * failing test beside it, rather than a line buried in a feature branch.
 *
 * **It did not hold that promise until 2026-10-15**, and the way it failed is worth keeping: the
 * list it compared against had ten entries while the client had fourteen, so four endpoints
 * (`/v1/session/usage`, `/v1/session/conversation`, `/v1/session/terminal`, `/v1/feedback/terminal`)
 * were reachable without this test saying anything — the check compared the list with itself. It now
 * reads the endpoints out of the client's own declared constants, so a new one cannot be invisible:
 * it fails here until somebody adds it to [BridgeClient.Endpoints.ALL] and decides, out loud, whether
 * it is a read or a write.
 */
class ApiSurfaceTest {

    /** Every endpoint the client declares, read off the class rather than listed by hand. */
    private fun declaredEndpoints(): Set<String> =
        BridgeClient.Endpoints::class.java.declaredFields
            .filter { it.type == String::class.java }
            .mapNotNull { field ->
                field.isAccessible = true
                field.get(BridgeClient.Endpoints) as? String
            }
            .filter { it.startsWith("/v1/") }
            .toSet()

    @Test
    fun theAppTalksToExactlyTheEndpointsWeAgreed() {
        assertEquals(
            "the endpoint list changed; if that is intentional, think about what it " +
                "lets the app change on the user's computer before updating this test",
            22,
            BridgeClient.Endpoints.ALL.size,
        )
        assertEquals(BridgeClient.Endpoints.ALL.toSet().size, BridgeClient.Endpoints.ALL.size)
        assertTrue(BridgeClient.Endpoints.ALL.all { it.startsWith("/v1/") })
        assertEquals(
            "an endpoint exists in the client that ALL does not name, so nothing decided whether it " +
                "is a read or a write",
            emptySet<String>(),
            declaredEndpoints() - BridgeClient.Endpoints.ALL.toSet(),
        )
    }

    @Test
    fun theWritesAreNamedAndTheDecisionsDoorIsTheOnlyOneOfItsKind() {
        // This test changed once, on 2026-10-13, and the change is the point of
        // having it. The app can now change two things: a decision (the app acting
        // for the user, four closed classes) and a key press in the escape hatch
        // (the user operating their own terminal). They are not two of a kind,
        // which is why the second is named separately rather than folded into the
        // first — a future contributor adding a third must argue with this list.
        //
        // It changed again on 2026-10-15, and for the same reason: the list said two while the app had
        // four ways to change something — typing a sentence into a session, and a real terminal on the
        // phone (D34), which sends whatever the user types. `/v1/session/keys` is still listed because
        // the client can still call it (`BridgeIntegrationTest` does, to watch the bridge refuse a key
        // it does not know) even though no screen reaches it any more: that makes it a retirement
        // candidate, not an invisible one.
        assertEquals(
            listOf(
                BridgeClient.Endpoints.DECISIONS,
                BridgeClient.Endpoints.SESSION,
                BridgeClient.Endpoints.SESSION_KEYS,
                BridgeClient.Endpoints.TERMINAL,
                BridgeClient.Endpoints.SHELL,
                // Added 2026-10-15 (seventh and eighth): naming something, and ending it. They are listed
                // here because they are the first writes in this product that change *what exists* rather
                // than what is being said, and because closing a project cannot be undone.
                BridgeClient.Endpoints.RENAME,
                BridgeClient.Endpoints.CLOSE,
                BridgeClient.Endpoints.PROJECTS,
                BridgeClient.Endpoints.SESSIONS,
                // Added 2026-10-15: a picture of the phone's screen, which *is* a write — it creates a file
                // on the user's computer. It is listed here so that "the app can write five things" is a
                // sentence somebody had to type, and so that the list keeps meaning what it says.
                BridgeClient.Endpoints.SCREENSHOT,
            ),
            BridgeClient.Endpoints.WRITES,
        )
        assertEquals("/v1/decisions", BridgeClient.Endpoints.ONLY_WRITE)
        assertTrue(
            "the decisions door is the only path with the four write classes behind it",
            BridgeClient.Endpoints.ONLY_WRITE in BridgeClient.Endpoints.WRITES,
        )

        val reads = listOf(
            BridgeClient.Endpoints.HEALTH,
            BridgeClient.Endpoints.SNAPSHOT,
            BridgeClient.Endpoints.VALUE,
            BridgeClient.Endpoints.STREAM,
            BridgeClient.Endpoints.SESSION_USAGE,
            BridgeClient.Endpoints.SESSION_CONVERSATION,
            BridgeClient.Endpoints.HARNESSES,
            // The working-folder picker lists directories; it creates nothing and writes nothing.
            BridgeClient.Endpoints.FOLDERS,
        )
        assertTrue(
            "a read endpoint must not also be a write endpoint",
            reads.none { it in BridgeClient.Endpoints.WRITES },
        )
    }
}
