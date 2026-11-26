package dev.perch.console.core.format

/**
 * Turns seconds into something a person would say.
 *
 * The numbers the product shows are claims about the user's time, so the wording
 * matters as much as the arithmetic: "about 8 minutes" is a claim someone can
 * check against their own experience, whereas "480s saved" invites distrust.
 * Rounding is deliberately coarse and always downwards-ish, so the figure never
 * flatters itself.
 */
object Duration {

    private const val MINUTE = 60
    private const val HOUR = 60 * MINUTE

    /**
     * A duration as a fragment, with no hedge attached.
     *
     * The hedge ("about", "roughly") belongs to the sentence, not to the fragment:
     * this used to return "about 10 minutes", and the weekly headline read
     * "about 10 minutes back this week" — a sentence starting in lower case, and one
     * that the claim line then had to repair by stripping the word back off. Copy
     * assembled by string surgery looks fine in code and wrong on screen.
     */
    fun humanise(seconds: Int): String {
        if (seconds <= 0) return "nothing yet"
        if (seconds < MINUTE) return "$seconds seconds"

        val minutes = seconds / MINUTE
        if (minutes < 60) return if (minutes == 1) "a minute" else "$minutes minutes"

        val hours = minutes / 60
        val leftover = minutes % 60
        val hourText = if (hours == 1) "an hour" else "$hours hours"
        return if (leftover < 5) hourText else "$hourText and $leftover minutes"
    }

    /**
     * The claim shown right after a decision.
     *
     * Deliberately says "seconds you did not spend at the computer" rather than
     * "time saved": the baseline is a median measured for doing the same thing at
     * the desk, and the wording should not promise more than that.
     */
    fun savedClaim(seconds: Int): String =
        if (seconds < MINUTE) {
            "Roughly $seconds seconds you did not spend walking back to the computer."
        } else {
            "Roughly ${humanise(seconds)} you did not spend at the computer."
        }
}
