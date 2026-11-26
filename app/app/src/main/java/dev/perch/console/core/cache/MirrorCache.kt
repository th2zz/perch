package dev.perch.console.core.cache

import dev.perch.console.core.herdrclient.Decision
import dev.perch.console.core.herdrclient.Mirror
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json

/**
 * A snapshot of the computer's state, kept so the app has something to show
 * before the network answers — and, more importantly, so it has something to
 * show when the phone is offline (spec FR-012).
 *
 * Everything here is disposable by design. The spec's acceptance criteria
 * include "clear the app's data and restart, and the view is still correct",
 * which only holds if nothing in the cache is authoritative. Nothing is: the
 * computer is re-read on every launch.
 */
@Serializable
data class CachedState(
    val mirror: Mirror = Mirror(),
    val decisions: List<Decision> = emptyList(),
    val savedAtMillis: Long = 0L,
)

class MirrorCache(private val file: java.io.File, private val json: Json) {

    fun read(): CachedState? =
        runCatching { json.decodeFromString<CachedState>(file.readText()) }.getOrNull()

    fun write(state: CachedState) {
        runCatching {
            file.parentFile?.mkdirs()
            // Write through a temp file: a crash mid-write must not leave a
            // half-parsed cache that the next launch would treat as truth.
            val temp = java.io.File(file.parentFile, "${file.name}.tmp")
            temp.writeText(json.encodeToString(state))
            if (!temp.renameTo(file)) {
                temp.copyTo(file, overwrite = true)
                temp.delete()
            }
        }
    }

    fun clear() {
        runCatching { file.delete() }
    }
}
