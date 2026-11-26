package dev.perch.console.core.outbox

import java.io.File
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * A reply the user wrote while the computer was unreachable.
 *
 * The distinction this type rests on is the difference between "the computer
 * said no" and "the computer did not answer". A refusal must never be queued —
 * retrying an action the computer has already rejected would hammer it with the
 * same refusal and, in the worst case, send a keystroke twice. A silence must
 * never be dropped — the user typed a sentence and believes it was sent.
 *
 * So only [Outcome.Unreachable] produces an entry here.
 */
@Serializable
data class QueuedReply(
    val decisionId: String,
    val paneId: String,
    val kind: String,
    val host: String = "",
    val text: String? = null,
    val queuedAtMillis: Long = 0L,
    val attempts: Int = 0,
    val lastError: String = "",
)

/**
 * Replies waiting for the computer to come back.
 *
 * Persisted, because "the phone was killed while the laptop was asleep" is a
 * completely ordinary sequence of events and losing the user's sentence there
 * would be the single most annoying way this product could fail.
 *
 * Delivery is deliberately not automatic on a timer: it happens when a
 * connection is confirmed, so a reply is never fired into a network that is up
 * but cannot reach the computer.
 */
class Outbox(private val file: File, private val json: Json) {

    private val lock = Any()

    fun list(): List<QueuedReply> = synchronized(lock) { read() }

    fun size(): Int = synchronized(lock) { read().size }

    fun enqueue(reply: QueuedReply) {
        synchronized(lock) {
            val current = read().toMutableList()
            // One entry per decision: if the user edits their mind before the
            // computer answers, the last thing they wrote is what they meant.
            current.removeAll { it.decisionId == reply.decisionId }
            current.add(reply)
            write(current)
        }
    }

    fun remove(decisionId: String) {
        synchronized(lock) {
            write(read().filterNot { it.decisionId == decisionId })
        }
    }

    /** Records a failed delivery attempt, keeping the entry for next time. */
    fun recordFailure(decisionId: String, error: String) {
        synchronized(lock) {
            write(
                read().map { entry ->
                    if (entry.decisionId == decisionId) {
                        entry.copy(attempts = entry.attempts + 1, lastError = error)
                    } else {
                        entry
                    }
                },
            )
        }
    }

    fun clear() {
        synchronized(lock) { write(emptyList()) }
    }

    private fun read(): List<QueuedReply> =
        runCatching { json.decodeFromString<List<QueuedReply>>(file.readText()) }
            .getOrDefault(emptyList())
            // An entry with no decision to attach to can never be delivered, and
            // keeping it would grow the file for ever.
            .filter { it.decisionId.isNotBlank() && it.paneId.isNotBlank() }

    private fun write(entries: List<QueuedReply>) {
        runCatching {
            file.parentFile?.mkdirs()
            val temp = File(file.parentFile, "${file.name}.tmp")
            temp.writeText(json.encodeToString(entries))
            if (!temp.renameTo(file)) {
                temp.copyTo(file, overwrite = true)
                temp.delete()
            }
        }
    }
}
