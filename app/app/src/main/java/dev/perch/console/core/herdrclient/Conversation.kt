package dev.perch.console.core.herdrclient

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class Transcript(
    /**
     * Whether the computer has this session's conversation to show.
     *
     * False means "draw the screen instead", which is what this face did before records existed and
     * is a better answer than an empty reading area: an agent whose records nobody has learned, and
     * an agent whose record could not be found, are both answered by the terminal's own text.
     */
    val readable: Boolean = false,
    /** Oldest first. Empty with [readable] true means nobody has typed yet. */
    val messages: List<ConversationMessage> = emptyList(),
    /** True while a turn is in flight. */
    val working: Boolean = false,
    /**
     * How long the current turn has been going, in seconds.
     *
     * Seconds rather than a timestamp because the host and the phone are two clocks, and a duration
     * computed from both is a number that can come out negative. The computer measured it; the phone
     * shows it.
     */
    @SerialName("working_secs") val workingSecs: Int = 0,
    /**
     * True when older messages exist but are not being sent.
     *
     * The conversation a phone is handed is bounded — a long session would otherwise be megabytes
     * over a mobile network — so the beginning can be missing, and the screen says so rather than
     * letting the reader believe they are looking at the start of it.
     */
    val truncated: Boolean = false,
) {
    /** True when the computer is readable and has nothing in it yet. */
    val isEmpty: Boolean get() = readable && messages.isEmpty()
}

/**
 * One thing worth reading: what the user asked, or what the agent finally answered.
 *
 * The step in between — the reasoning, the commands, the edits — is deliberately not on this face.
 * On a phone it was collapsed behind its own expander anyway, so all it did was make the answer
 * hard to find.
 */
@Serializable
data class ConversationMessage(
    val role: String = "",
    val text: String = "",
) {
    /** True when this is the agent's own words rather than the user's. */
    val isAgent: Boolean get() = role == Speakers.AGENT
}

/**
 * Who said a message.
 *
 * Two strings that travel on the wire and are compared on this side, so they live in the shared
 * vocabulary file with the other value sets rather than being written inline: a rename would not
 * raise anything, it would quietly make every message look like the user's.
 */
object Speakers {
    const val YOU = "you"
    const val AGENT = "agent"

    /** Pinned against the shared vocabulary by `WireVocabularyContractTest`. */
    val all = listOf(YOU, AGENT)
}
