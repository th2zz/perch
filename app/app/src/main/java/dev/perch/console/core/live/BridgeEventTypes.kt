package dev.perch.console.core.live

object BridgeEventTypes {

    /** The whole world may have moved; re-read. */
    const val SNAPSHOT = "snapshot"

    /** A new request is waiting for a person. */
    const val DECISION = "decision"

    /** A request is no longer waiting — answered here, on the computer, or expired. */
    const val RESOLVED = "resolved"

    /** The bridge's own view may have fallen behind; re-read rather than trust. */
    const val RESYNC = "resync"

    /**
     * The transport's keepalive. It carries no meaning for the queue and is
     * ignored: it exists so an idle connection is not mistaken for a dead one.
     */
    const val HEARTBEAT = "heartbeat"

    /**
     * Every type, kept in step with the shared contract by a test.
     *
     * The order is the bridge's order (`core.AllEventTypes`) so the two lists read
     * the same way when someone compares them.
     */
    val all = listOf(SNAPSHOT, DECISION, RESOLVED, RESYNC, HEARTBEAT)

    /**
     * The types that mean "the queue may have moved, go and look".
     *
     * Everything except the heartbeat: a keepalive says the socket is alive, not
     * that anything happened, and re-reading on it would turn a quiet connection
     * into a request every thirty seconds — the shape of the loop this codebase has
     * already had to fix once.
     */
    val queueMoving = listOf(DECISION, RESOLVED, RESYNC, SNAPSHOT)

    /** True when this type is a reason to re-read the computer. */
    fun movesTheQueue(type: String): Boolean = type in queueMoving
}
