package dev.perch.console

import dev.perch.console.core.outbox.Outbox
import dev.perch.console.core.outbox.QueuedReply
import java.io.File
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * The replies the app holds when the computer is asleep.
 *
 * This is the part of the product that must never lose the user's words: it
 * fails precisely when they are away from the desk, which is the whole point of
 * the product, and it fails silently, which is the worst way to fail.
 */
class OutboxTest {

    @get:Rule
    val folder = TemporaryFolder()

    private val json = Json { ignoreUnknownKeys = true; explicitNulls = false }

    private fun outbox(file: File = File(folder.root, "outbox.json")) = Outbox(file, json)

    private fun reply(id: String, text: String? = "yes, go ahead") = QueuedReply(
        decisionId = id,
        paneId = "wA:p1",
        kind = "answer",
        text = text,
        queuedAtMillis = 1_760_000_000_000,
    )

    @Test
    fun aReplySurvivesTheProcessBeingKilled() {
        val file = File(folder.root, "outbox.json")
        outbox(file).enqueue(reply("d1"))

        // A new instance is what the app has after Android kills the process while
        // the laptop was asleep — the ordinary sequence, not an edge case.
        val reopened = outbox(file)
        assertEquals(1, reopened.size())
        assertEquals("yes, go ahead", reopened.list().first().text)
    }

    @Test
    fun oneEntryPerDecisionSoTheLastThingWrittenIsWhatIsMeant() {
        val store = outbox()
        store.enqueue(reply("d1", "first thought"))
        store.enqueue(reply("d1", "second thought"))
        store.enqueue(reply("d2", "for the other one"))

        val entries = store.list()
        assertEquals(2, entries.size)
        assertEquals(
            "second thought",
            entries.first { it.decisionId == "d1" }.text,
        )
    }

    @Test
    fun aDeliveredReplyLeavesAndAFailedOneStays() {
        val store = outbox()
        store.enqueue(reply("d1"))
        store.enqueue(reply("d2"))

        store.remove("d1")
        store.recordFailure("d2", "no route to host")

        val entries = store.list()
        assertEquals(1, entries.size)
        assertEquals("d2", entries.first().decisionId)
        assertEquals(1, entries.first().attempts)
        assertEquals("no route to host", entries.first().lastError)

        store.recordFailure("d2", "still nothing")
        assertEquals(2, store.list().first().attempts)
    }

    @Test
    fun anUndeliverableEntryIsDroppedRatherThanKeptForEver() {
        val file = File(folder.root, "outbox.json")
        // An entry with nothing to attach to can never be delivered, so it is
        // dropped rather than kept for ever. `kind` is required on purpose: an
        // entry that does not say what it is asking for is not deliverable either.
        file.writeText(
            """
            [
              {"decisionId":"","paneId":"","kind":"answer"},
              {"decisionId":"d9","paneId":"wA:p1","kind":"answer"}
            ]
            """.trimIndent(),
        )

        val entries = outbox(file).list()
        assertEquals(1, entries.size)
        assertEquals("d9", entries.first().decisionId)
    }

    @Test
    fun anEntryMissingItsKindIsAlsoDropped() {
        val file = File(folder.root, "outbox.json")
        // `kind` has no default, so a half-written entry fails to decode and the
        // whole file reads as empty. That is the safe direction: the alternative
        // is inventing an action the user never asked for.
        file.writeText("""[{"decisionId":"d9","paneId":"wA:p1"}]""")
        assertTrue(outbox(file).list().isEmpty())
    }

    @Test
    fun aCorruptFileIsTreatedAsEmptyInsteadOfCrashing() {
        val file = File(folder.root, "outbox.json")
        file.writeText("{ this is not json")

        assertTrue(outbox(file).list().isEmpty())
        // And it must still be usable afterwards.
        val store = outbox(file)
        store.enqueue(reply("d1"))
        assertEquals(1, store.size())
    }
}
