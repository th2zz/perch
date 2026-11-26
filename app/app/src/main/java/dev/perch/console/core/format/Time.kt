package dev.perch.console.core.format

import java.time.Duration
import java.time.Instant
import java.time.OffsetDateTime

/**
 * Turns the bridge's RFC 3339 timestamps into something a person reads.
 *
 * The bridge writes Go's default time format, which includes nanoseconds and an
 * offset — parseable, but not something to show. Anything unparseable returns an
 * empty string rather than "Invalid date": if the clock format ever changes, the
 * right failure is silence, not noise on a card.
 */
object Time {

    fun parse(timestamp: String): Instant? =
        runCatching { OffsetDateTime.parse(timestamp).toInstant() }.getOrNull()

    /** "just now", "4 min ago", "2 h ago" — coarse on purpose. */
    fun ago(timestamp: String, now: Instant = Instant.now()): String {
        val then = parse(timestamp) ?: return ""
        val elapsed = Duration.between(then, now)
        val minutes = elapsed.toMinutes()
        return when {
            elapsed.isNegative -> ""
            minutes < 1 -> "just now"
            minutes < 60 -> "$minutes min ago"
            minutes < 60 * 24 -> "${minutes / 60} h ago"
            else -> "${minutes / (60 * 24)} d ago"
        }
    }

    /** How long the user has left to act, given the queue's TTL in seconds. */
    fun remaining(timestamp: String, ttlSeconds: Long, now: Instant = Instant.now()): Duration? {
        val then = parse(timestamp) ?: return null
        val left = Duration.ofSeconds(ttlSeconds).minus(Duration.between(then, now))
        return if (left.isNegative) Duration.ZERO else left
    }
}
