package dev.perch.console

import dev.perch.console.core.herdrclient.BridgeClient
import dev.perch.console.features.terminal.TerminalConnection
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.flow.filterIsInstance
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.flow.collect
import kotlinx.coroutines.flow.takeWhile
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.withTimeout
import org.junit.Assert.assertTrue
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assume.assumeTrue
import org.junit.BeforeClass
import org.junit.Test

/**
 * The app's own terminal client against the real bridge and the real herdr.
 *
 * This is the only level where the pair can be checked at all: the Go side has its own verification
 * (`scripts/verify-terminal.sh`) that proves the wire carries bytes and typing, and this proves the
 * *client the app ships* speaks that wire — handshake, authentication, frame types, and the fact that
 * attaching produces a screen.
 *
 * It is deliberately read-only: it attaches, waits for the repaint, and stops. Typing is what the
 * bridge's verification does, in a scratch session of its own; a test that typed here would type into
 * whichever session the developer happens to have open, which is not a thing a test may do.
 *
 * Skipped when the bridge binary or herdr is not available — the same conditions as the rest of the
 * integration suite.
 */
class TerminalConnectionTest {

    companion object {
        @BeforeClass
        @JvmStatic
        fun startBridge() = BridgeHarness.start()
    }

    private fun requireBridge() {
        BridgeHarness.start()
        assumeTrue("the bridge binary was not available for this build", BridgeHarness.available)
        assumeTrue("herdr is not running on this machine", BridgeHarness.herdrRunning)
    }

    /**
     * Attaching to a pane produces the screen.
     *
     * The assertion can be exact because of a property measured on the host: a fresh attach makes the
     * terminal repaint the whole screen, so bytes are guaranteed to arrive. That is also why
     * reconnecting is free, and why nothing on either side has to store a backlog.
     */
    @Test
    fun attachingProducesTheScreen() {
        requireBridge()
        val client = BridgeClient(BridgeHarness.baseUrl, BridgeHarness.token)

        // A pane that exists right now, read from the mirror the bridge already keeps.
        val pane = runBlocking {
            val mirror = client.snapshot()
            mirror.mirror.panes.firstOrNull()?.paneId
        }
        assumeTrue("no panes on this machine to attach to", pane != null)

        val connection = TerminalConnection(
            client = client,
            paneId = pane!!,
            hostId = "",
        )

        // One attach, two claims about it — and deliberately one *attach*, because this test runs
        // against the developer's real herdr session and a pane takes one control client at a time.
        // (Measured: two of these tests attaching to the same pane at once made one of them time out.)
        //
        // The claims are the two halves of the wire: the bridge says how big the pane is — which is
        // what replaced "the phone says how big the pane should be" — and the terminal's own bytes
        // follow. `first` rather than `collect` for each: the flow owns the socket while it is
        // collected, so the test has to finish collecting rather than merely stop looking.
        val (shape, bytes) = runBlocking {
            withTimeout(TimeUnit.SECONDS.toMillis(20)) {
                var shape: TerminalConnection.Event.Shape? = null
                var bytes: TerminalConnection.Event.Bytes? = null
                // One collection, because the flow owns one socket: collecting it twice would attach
                // twice. `takeWhile` ends it (and closes the socket) the moment both have arrived.
                connection.events()
                    .takeWhile { event ->
                        when (event) {
                            is TerminalConnection.Event.Shape -> shape = event
                            is TerminalConnection.Event.Bytes -> bytes = event
                            else -> Unit
                        }
                        shape == null || bytes == null
                    }
                    .collect {}
                checkNotNull(shape) to checkNotNull(bytes)
            }
        }

        assertTrue("a terminal with no columns is not a screen: ${shape.cols}", shape.cols > 1)
        assertTrue("a terminal with no rows is not a screen: ${shape.rows}", shape.rows > 1)
        assertTrue("attaching produced no bytes at all", bytes.payload.isNotEmpty())
    }

    /**
     * The geometry message is the bridge's own, so it has to be read strictly: anything else on the
     * text channel (herdr's CLI writes its own notices there) must not be mistaken for a shape, or the
     * emulator would be resized to a number somebody wrote in a log line.
     */
    @Test
    fun onlyTheGeometryMessageIsReadAsAShape() {
        assertEquals(
            TerminalConnection.Event.Shape(cols = 183, rows = 58),
            TerminalConnection.shapeOf("""{"type":"size","cols":183,"rows":58}"""),
        )
        for (other in listOf(
            "herdr: attached",
            """{"type":"size"}""",
            """{"type":"size","cols":0,"rows":58}""",
            """{"type":"size","cols":"183","rows":58}""",
            """{"bytes":"AAAA"}""",
            "",
        )) {
            assertNull("$other is not a shape", TerminalConnection.shapeOf(other))
        }
    }
}
