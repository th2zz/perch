package dev.perch.console.core.herdrclient

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class UsageFacts(
    val harness: String = "",
    val model: String = "",
    val effort: String = "",
    @SerialName("context_used") val contextUsed: Long = 0,
    @SerialName("context_window") val contextWindow: Long = 0,
    @SerialName("total_tokens") val totalTokens: Long = 0,
    @SerialName("cached_tokens") val cachedTokens: Long = 0,
    val access: String = "",
    val goal: String = "",
    @SerialName("goal_status") val goalStatus: String = "",
    @SerialName("session_id") val sessionId: String = "",
    /**
     * Whether Perch knows how to read this agent's records at all.
     *
     * Deliberately separate from "recorded nothing": the panel has one sentence for "we have not
     * learned this agent yet" and another for "it has not written anything down", and merging
     * them into one empty box is the small dishonesty that makes people stop trusting the rest
     * of the screen.
     */
    val readable: Boolean = false,
) {
    /** True when the computer had nothing to say about this session. */
    val isEmpty: Boolean
        get() = model.isBlank() && contextUsed == 0L && totalTokens == 0L &&
            goal.isBlank() && access.isBlank()
}
