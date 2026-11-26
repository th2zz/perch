package dev.perch.console.features.terminal

import dev.perch.console.core.herdrclient.SessionView
import dev.perch.console.core.herdrclient.Transcript
import dev.perch.console.core.herdrclient.WireValues

object LiveSession {

    const val POLL_MILLIS_WORKING = 800L

    /**
     * How often an open session is re-read when nothing is being written.
     *
     * Not never: the phone is not the only thing that can change a session — a person at the
     * keyboard, another agent, a dialog appearing — and a screen that has quietly stopped
     * being informed is the failure this whole file exists to prevent. Three seconds costs
     * little and still notices.
     */
    const val POLL_MILLIS_IDLE = 3000L

    /**
     * How long a catch-up scroll takes.
     *
     * The refresh used to move the view instantly, which is why streamed output arrived as a
     * series of jumps rather than as writing. A short glide is the difference between "the
     * screen keeps jerking" and "the text is coming in".
     */
    const val FOLLOW_SCROLL_MILLIS = 150

    /**
     * The wait before the next read, for a session in a given state.
     *
     * Pure and here rather than inline in the loop so the rule can be read and tested in
     * one place: only an agent that is *working* earns the fast interval, because only
     * then is the screen changing under the user's eyes.
     */
    fun pollInterval(agentStatus: String): Long =
        if (agentStatus == WireValues.AgentStatus.WORKING) POLL_MILLIS_WORKING else POLL_MILLIS_IDLE

    const val READ_LINES = 1000

    const val MIN_POLL_LINES = 160

    /**
     * How much a poll re-reads: everything on screen, and never less than [MIN_POLL_LINES].
     *
     * Pure, and here rather than inline in the loop, because this is the whole fix and a rule nobody
     * can read is a rule that comes back. The measured cost of asking for this much is small next to
     * the cost of losing it: the same pane through the bridge's API was 8.4 KB at 160 lines against
     * 19.6 KB for the whole 411, once per poll while an agent is writing.
     */
    fun pollDepth(shown: String): Int =
        maxOf(MIN_POLL_LINES, shown.lines().size)

    const val FOLLOW_SLACK = 8

    fun afterRead(shown: SessionView, fresh: SessionView, following: Boolean): SessionView =
        if (following) {
            fresh
        } else {
            fresh.copy(
                text = shown.text,
                truncated = shown.truncated,
            )
        }

    /**
     * True when the second read says something different from the first.
     *
     * Deliberately *not* `previous != next`. Every read carries a fresh `read_at`,
     * and a read may carry a new `revision`, both of which change on every poll by
     * construction. Comparing whole objects would therefore repaint the screen,
     * re-parse the Markdown and — before the follow rule below — move the scroll
     * position forty times a minute while the agent sat perfectly still. So this
     * compares what the screen draws, and nothing else.
     */
    fun differs(previous: SessionView, next: SessionView): Boolean =
        previous.text != next.text ||
            previous.agentStatus != next.agentStatus ||
            previous.agentLabel != next.agentLabel ||
            previous.title != next.title ||
            previous.rule != next.rule ||
            previous.decisionId != next.decisionId ||
            previous.truncated != next.truncated ||
            previous.unsent != next.unsent ||
            previous.menu != next.menu ||
            previous.status != next.status ||
            previous.chromeLines != next.chromeLines ||
            previous.workspaceLabel != next.workspaceLabel ||
            previous.hostLabel != next.hostLabel ||
            previous.waitingOnAPerson != next.waitingOnAPerson ||
            previous.approvable != next.approvable ||
            previous.rejectable != next.rejectable ||
            previous.answerable != next.answerable

    /**
     * True when a scroll position is at the end, so new text should stay in view.
     *
     * The rule exists because the two things a user does with this screen pull in
     * opposite directions: reading what the agent is saying *now* needs the newest
     * line to stay on screen, and reading what it said five minutes ago needs the
     * view to not move under their finger. A view already at the end follows; a view
     * the user has scrolled away from stays exactly where they left it, and comes
     * back to following the moment they scroll down again.
     */
    fun following(offset: Int, max: Int, slack: Int = FOLLOW_SLACK): Boolean =
        max - offset <= slack

    // ---------------------------------------------------------------- the conversation

    /**
     * True when the second read of a conversation says something different from the first.
     *
     * The same rule as [differs] for the screen, and for the same reason: a read that changed nothing
     * must not repaint the reading area, or the phone spends its battery re-laying-out an answer
     * nobody changed. The one field that *is* compared although it changes by itself is `workingSecs`
     * — that is the clock under "working · 2m", and it changing once a second is the point of it.
     */
    fun conversationDiffers(previous: Transcript, next: Transcript): Boolean =
        previous.readable != next.readable ||
            previous.working != next.working ||
            previous.workingSecs != next.workingSecs ||
            previous.truncated != next.truncated ||
            previous.messages != next.messages

    /**
     * Whether to say that the agent is working, and for how long.
     *
     * The computer's own record is asked first, because it knows the difference between an agent that
     * is thinking and one that has stopped — herdr's status is about the pane, and a pane stays
     * "working" through the pause between an agent finishing and its next line arriving. When there is
     * no record to ask (an agent whose records nobody has learned), herdr's status is the only thing
     * there is, and it is better than saying nothing.
     *
     * Seconds, and 0 when the duration is unknown: the label then says "working" with no time, which
     * is honest, rather than a number made up by the phone.
     */
    fun isWorking(conversation: Transcript?, agentStatus: String): Boolean =
        if (conversation?.readable == true) {
            conversation.working
        } else {
            agentStatus == WireValues.AgentStatus.WORKING
        }

    fun workingSeconds(conversation: Transcript?): Int =
        if (conversation?.readable == true) conversation.workingSecs else 0
}
