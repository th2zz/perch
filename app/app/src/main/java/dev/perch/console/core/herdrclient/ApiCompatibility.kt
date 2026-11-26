package dev.perch.console.core.herdrclient

import androidx.annotation.StringRes
import dev.perch.console.R

/**
 * Whether this app and the computer agree on what a request looks like.
 *
 * The spec calls this out as its own edge case, and it is worth its own file because
 * the two halves are updated by different people: the bridge on the user's machine,
 * the app whenever they get around to it. The failure without this check is
 * miserable to diagnose — a new app talking to an old bridge gets a flat
 * `400 {"error":"expected {pane_id, keys}"}`, which reads as "the app is broken"
 * rather than "one of your two halves is older than the other".
 *
 * Two numbers travel: the version people read, and the API version, which exists to
 * be compared. Only the second one is used here.
 */
enum class ApiCompatibility {
    /** Same contract. */
    Ok,

    /** The computer's bridge is older than this app. */
    BridgeTooOld,

    /** This app is older than the computer's bridge. */
    AppTooOld,

    /**
     * The computer did not say which contract it speaks.
     *
     * Its own value: an older bridge has no such field, and claiming incompatibility
     * from an absence would be a guess (spec FR-015). Nothing is blocked in this
     * case; the requests are simply attempted, and a refusal is reported as one.
     */
    Unknown,
    ;

    val isMismatch: Boolean get() = this == BridgeTooOld || this == AppTooOld

    /**
     * The sentence to show, or null when there is nothing to say.
     *
     * Returning the reason *and* what to update is the whole point: "incompatible
     * versions" without saying which side to move leaves the user with a puzzle.
     */
    @StringRes
    fun messageResource(): Int? = when (this) {
        Ok, Unknown -> null
        BridgeTooOld -> R.string.failure_bridge_too_old
        AppTooOld -> R.string.failure_app_too_old
    }

    @StringRes
    fun fixResource(): Int? = when (this) {
        Ok, Unknown -> null
        BridgeTooOld -> R.string.failure_bridge_too_old_fix
        AppTooOld -> R.string.failure_app_too_old_fix
    }

    companion object {
        /**
         * The contract this build speaks.
         *
         * It must match `bridge/internal/server/version.go`. The two are compared by
         * a test on the bridge side and by the numbers here, not by a shared file:
         * a generator would be more moving parts than the single integer it would
         * generate.
         */
        /**
         * Contract 2: the working folder became a required, validated part of creation.
         *
         * A bridge at 1 silently made a blank `cwd` mean home; this app cannot offer the picker
         * against it (`/v1/folders` does not exist there). The two halves move together, which is
         * exactly what this number is for.
         */
        const val API_VERSION = 2

        /**
         * Compares what the computer said with what this app speaks.
         *
         * `appApiVersion` is a parameter rather than a direct read of [API_VERSION]
         * for a reason worth stating: while the contract is at version 1, "a bridge
         * one version older" is *not representable* — one older is zero, and zero is
         * reserved for "the computer did not say". Testing the branch therefore needs
         * the seam, and a branch that cannot be tested is a branch nobody should
         * trust on the day it first runs.
         */
        fun of(bridgeApiVersion: Int, appApiVersion: Int = API_VERSION): ApiCompatibility = when {
            bridgeApiVersion <= 0 -> Unknown
            bridgeApiVersion == appApiVersion -> Ok
            bridgeApiVersion < appApiVersion -> BridgeTooOld
            else -> AppTooOld
        }
    }
}
