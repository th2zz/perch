package dev.perch.console

import dev.perch.console.features.terminal.ShellConnection
import dev.perch.console.features.terminal.ShellRetry
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The shell's control channel, read strictly — and the one bit that makes it behave like mosh.
 *
 * The bridge sends exactly one text frame on this socket, and only when the *shell itself* has stopped:
 * `{"type":"ended","reason":…}`. Everything else on the wire is bytes. The reader is strict about that, in
 * the same way the pane stream's geometry reader is strict, because the two ways to be loose both end
 * badly: a text frame that is not ours becomes words on the screen that nobody wrote for the user, and a
 * reason read out of the wrong shape becomes a sentence that hides the real cause ("the shell ended:
 * closed" when what happened was that the computer stopped answering).
 *
 * The distinction the tests below hold on to is the one a phone lives with: a *stated* end means the shell
 * is finished and only a new terminal will do, while a *silent* end (a dropped socket, a phone that walked
 * into a tunnel, a client that fell behind and was dropped on purpose) means the shell is still running and
 * the screen should attach again. Getting that backwards is either a terminal that dies in a lift or one
 * that quietly replaces a running build.
 */
class ShellConnectionTest {

    @Test
    fun theBridgesEndMessageIsRead() {
        val ended = ShellConnection.endedOf("""{"type":"ended","reason":"exit status 130"}""")

        assertEquals("exit status 130", ended?.reason)
        assertTrue("a stated end is the end of the shell", ended?.gone == true)
    }

    /** A missing reason still means the shell stopped, and says so in words rather than an empty line. */
    @Test
    fun anEndWithNoReasonStillSaysSomething() {
        assertEquals("the shell exited", ShellConnection.endedOf("""{"type":"ended"}""")?.reason)
        assertEquals("the shell exited", ShellConnection.endedOf("""{"type":"ended","reason":"  "}""")?.reason)
    }

    /** Anything that is not the bridge's end message is not an event, whatever it looks like. */
    @Test
    fun otherTextFramesAreNotEvents() {
        assertNull(ShellConnection.endedOf("""{"type":"size","cols":80,"rows":24}"""))
        assertNull(ShellConnection.endedOf("""{"error":"unreadable control message"}"""))
        assertNull(ShellConnection.endedOf("not json at all"))
        assertNull(ShellConnection.endedOf(""))
    }

    /**
     * The retry ladder: fast enough to blink, slow enough for a tunnel.
     *
     * Both ends are load-bearing and both were a decision rather than a default. A first retry a second
     * later is what makes a phone switching from Wi-Fi to cellular look like a hiccup; a cap of eight
     * seconds is what stops an hour in a tunnel from becoming an hour of requests every two seconds. It is
     * also monotonic, so a phone that keeps failing does not hammer the bridge while the user is asleep.
     */
    @Test
    fun theRetryLadderStartsFastAndHasACap() {
        assertEquals(1_000L, ShellRetry.delayFor(0))
        assertTrue("it must slow down", ShellRetry.delayFor(1) > ShellRetry.delayFor(0))
        assertTrue("and keep slowing down", ShellRetry.delayFor(3) > ShellRetry.delayFor(1))
        // The cap is the point of the ladder: attempt 40 is an hour in a tunnel, not a reason to hammer.
        assertEquals(ShellRetry.delayFor(3), ShellRetry.delayFor(40))
        assertTrue("a cap that grows forever is not a cap", ShellRetry.delayFor(40) <= 8_000L)
        assertFalse("the ladder must not start at zero delay", ShellRetry.delayFor(0) <= 0)
    }
}
