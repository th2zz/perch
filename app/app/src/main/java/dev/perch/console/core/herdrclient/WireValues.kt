package dev.perch.console.core.herdrclient

/**
 * The closed value sets the app compares against, named instead of typed inline.
 *
 * These are not enums, and that is deliberate: herdr may introduce a state this
 * build has never heard of, and a decoding failure would turn "we do not know"
 * into "the app is broken". So every one of these fields is a `String` — which
 * means the *comparison* is the dangerous part, and it is where a rename does its
 * damage. `decisions.filter { it.state == "pending" }` decides what the queue
 * screen shows; `AgentStates.needsDecision(status)` decides whether the phone says
 * anyone is waiting. Rename the value on the bridge and nothing raises: the list
 * comes back empty, and the phone reports that there is nothing to do while the
 * agents wait. The loudest failure in the quietest form.
 *
 * So the strings live here, and
 * `contracts/wire-vocabulary.json` — written by the bridge,
 * read by both sides' tests — pins them. A one-sided change turns a test red
 * rather than a phone quiet.
 *
 * [DecisionKind] carries its own vocabulary and is checked
 * against the same file rather than duplicated here.
 */
object WireValues {

    /** What an agent's `agent_status` can be on the wire (D55 ④). */
    object AgentStatus {
        const val IDLE = "idle"
        const val WORKING = "working"
        const val BLOCKED = "blocked"
        const val DONE = "done"
        const val UNKNOWN = "unknown"

        val all = listOf(IDLE, WORKING, BLOCKED, DONE, UNKNOWN)

        fun needsDecision(status: String): Boolean = status == BLOCKED
    }

    /** What `Decision.state` can be. Terminal states mean it is no longer waiting. */
    object DecisionState {
        /** The agent is waiting and nobody has answered. */
        const val PENDING = "pending"

        /** The user answered it from this phone. */
        const val HANDLED_LOCALLY = "handled_locally"

        /** The agent resumed without us — almost always handled on the computer. */
        const val HANDLED_ELSEWHERE = "handled_elsewhere"

        /** It sat unanswered past its time to live. */
        const val EXPIRED = "expired"

        val all = listOf(PENDING, HANDLED_LOCALLY, HANDLED_ELSEWHERE, EXPIRED)
    }

    /** What `Host.status` can be. */
    object HostStatus {
        /** The last read of this computer succeeded. */
        const val REACHABLE = "reachable"

        /** We looked and it did not answer. */
        const val UNREACHABLE = "unreachable"

        /** We have not looked yet, which is not the same as failing. */
        const val UNKNOWN = "unknown"

        val all = listOf(REACHABLE, UNREACHABLE, UNKNOWN)
    }
}
