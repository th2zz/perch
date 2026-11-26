package dev.perch.console.core.errors

import dev.perch.console.core.herdrclient.BridgeException
import java.io.IOException

/**
 * What kind of thing went wrong, in terms the user's options depend on.
 *
 * The reason this is its own type rather than a pile of string comparisons: the
 * product promises that every failure comes with a next step (spec FR-015), and
 * the *step* follows from the kind, not from the message text. A refusal means
 * answer it another way; a silence means try again later; an expired session means
 * pair again. Getting that mapping wrong is how an app ends up telling someone to
 * "check your network" when the real answer is "your request is no longer open".
 *
 * Classification is pure so it can be tested without a device, and the wording
 * lives in string resources so it can be translated without touching this file.
 */
enum class FailureKind {
    /** The phone has no pairing, or the pair is unusable. */
    NotPaired,

    /** The computer did not answer: asleep, off the network, bridge not running. */
    Unreachable,

    /** The computer answered, and refused. The reason is in the message. */
    Refused,

    /** The request is no longer open — usually because it was handled elsewhere. */
    Stale,

    /** This app and the computer disagree about something structural. */
    Incompatible,

    /** Anything we did not anticipate. */
    Unknown,
}

object Failures {

    /** Classifies one failure. Never throws: it is called on the error path. */
    fun classify(error: Throwable): FailureKind = when {
        error !is BridgeException -> if (error is IOException) FailureKind.Unreachable else FailureKind.Unknown
        error.httpStatus == 0 -> FailureKind.Unreachable
        error.httpStatus == 401 || error.httpStatus == 403 -> FailureKind.NotPaired
        error.httpStatus == 404 || error.httpStatus == 409 -> FailureKind.Stale
        error.httpStatus == 422 -> FailureKind.Refused
        error.httpStatus == 400 -> FailureKind.Incompatible
        error.httpStatus >= 500 -> FailureKind.Unreachable
        else -> FailureKind.Unknown
    }

    /**
     * True when trying the same thing again later is a sensible thing to suggest.
     *
     * This is the one distinction the product cannot get wrong: a refusal retried
     * is a same-answer loop, and a silence dropped is a sentence the user typed
     * that never arrived.
     */
    fun worthRetrying(kind: FailureKind): Boolean =
        kind == FailureKind.Unreachable
}
