package dev.perch.console

import android.os.Looper
import androidx.test.core.app.ApplicationProvider
import com.sun.net.httpserver.HttpServer
import dev.perch.console.core.herdrclient.ApiCompatibility
import dev.perch.console.core.live.BridgeStream
import dev.perch.console.features.console.ConsoleState
import dev.perch.console.features.console.ConsoleViewModel
import java.net.InetSocketAddress
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.BeforeClass
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config

/**
 * The one action a new user takes, and the one that had no test at all.
 *
 * Everything around it was covered separately — the link parses, the manifest
 * accepts the tap, the screen renders, the compatibility rule is a tested pure
 * function, the token store has its own resilience tests — but the *action* that
 * joins them up had nothing. That is the shape of gap this codebase keeps finding:
 * every piece green, the seam unmeasured, and SC-004 ("first connection: zero
 * inputs, under three minutes") resting on it entirely.
 *
 * **What a JVM cannot check, and why.** A phone completes the last step by
 * encrypting the token with a key that never leaves the device — `AndroidKeyStore`,
 * which Robolectric does not have. So the happy path ends at the store here, and
 * one of these tests asserts exactly that: pairing must reach the store against a
 * real computer, and the *only* acceptable failure is the one the store itself
 * reports. That still catches a break anywhere earlier in the chain (a health
 * check, a compatibility verdict), which is where the interesting code is. The
 * encryption itself is a device-level fact, on the same list as vendor push
 * delivery.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PairingActionTest {

    companion object {
        @BeforeClass
        @JvmStatic
        fun startBridge() = BridgeHarness.start()
    }

    private val app: PerchApp = ApplicationProvider.getApplicationContext()

    private var stub: HttpServer? = null

    @After
    fun cleanUp() {
        // BridgeStream is a singleton: a stream left running would follow the next
        // test around, and a leaked connection is how a green suite becomes an
        // unreliable one.
        BridgeStream.stop()
        stub?.stop(0)
    }

    /**
     * Runs [action] and pumps the main looper until its callback fires.
     *
     * The health check happens off the main dispatcher and the state change on it,
     * and Robolectric's looper is paused, so the test has to let the main thread do
     * that work. Bounded, and the failure says what did not happen rather than
     * timing out silently.
     */
    private fun awaitResult(action: (onResult: (String?) -> Unit) -> Unit): String? {
        var finished = false
        var outcome: String? = null
        action { error ->
            outcome = error
            finished = true
        }
        val deadline = System.currentTimeMillis() + 15_000
        while (!finished && System.currentTimeMillis() < deadline) {
            shadowOf(Looper.getMainLooper()).idle()
            if (!finished) Thread.sleep(20)
        }
        assertTrue("the pairing attempt never reported back (neither succeeded nor failed)", finished)
        return outcome
    }

    /** A computer that answers, but speaks a different contract. */
    private fun startStubBridge(apiVersion: Int): String {
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/v1/health") { exchange ->
            val body = """{"bridge_version":"stub","api_version":$apiVersion,"health":{"items":[],"overall":"ok"}}"""
            val bytes = body.toByteArray()
            exchange.responseHeaders.add("Content-Type", "application/json")
            exchange.sendResponseHeaders(200, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        stub = server
        return "http://127.0.0.1:${server.address.port}"
    }

    @Test
    fun pairingAgainstARealComputerReachesTheStoreAndNowhereElse() {
        assumeTrue("the bridge binary was not available for this build", BridgeHarness.available)

        val viewModel = ConsoleViewModel(app)
        val error = awaitResult { viewModel.pair(BridgeHarness.baseUrl.toString(), BridgeHarness.token, it) }

        // Success is fine too: it would mean this JVM grew a keystore. What is not
        // fine is failing for any *other* reason — that would mean the chain before
        // the store broke.
        val storeFailed = app.getString(R.string.pair_store_failed)
        assertTrue(
            "pairing must get all the way to storing the connection; it stopped earlier with: $error",
            error == null || error.startsWith(storeFailed),
        )
    }

    @Test
    fun anAddressThatAnswersNothingIsRefusedAndNothingIsStored() {
        val viewModel = ConsoleViewModel(app)
        // Port 9 is "discard": nothing listens there, and the connection fails
        // immediately rather than hanging.
        val error = awaitResult { viewModel.pair("http://127.0.0.1:9", "a-token-nobody-issues", it) }

        assertNotNull("a computer that never answered must produce a message, not silence", error)
        assertNull("a failed attempt must not be stored as a pairing", app.container.pairingStore.load())
        assertTrue(
            "the user must not be left on a screen that looks connected",
            viewModel.state.value.phase != ConsoleState.Phase.Ready,
        )
    }

    @Test
    fun aComputerSpeakingADifferentContractIsRefusedBeforeAnythingIsStored() {
        // The spec makes this its own edge case, and put it on the *pairing* screen
        // for a reason: the alternative is that the user pairs, and then the first
        // button they press answers `400 expected {pane_id, keys}`, which reads as
        // "this app is broken" rather than "one of your two halves is older".
        val address = startStubBridge(apiVersion = 999)
        val viewModel = ConsoleViewModel(app)
        val error = awaitResult { viewModel.pair(address, "a-token", it) }

        val expected = app.getString(ApiCompatibility.AppTooOld.messageResource()!!)
        assertNotNull("an incompatible computer must be refused, not connected to", error)
        assertTrue("the message must say which half is older, got: $error", error!!.startsWith(expected))
        assertNull("nothing may be stored from a refused pairing", app.container.pairingStore.load())
        assertTrue(
            "the screen must stay on the pairing form",
            viewModel.state.value.phase != ConsoleState.Phase.Ready,
        )
    }

    @Test
    fun somethingThatIsNotAnAddressIsRefusedWithoutTrying() {
        val viewModel = ConsoleViewModel(app)
        val error = awaitResult { viewModel.pair("   ", "a-token", it) }

        assertEquals(
            "a blank address has one sensible answer, and it is not a network error",
            app.getString(R.string.pair_bad_address),
            error,
        )
        assertNull(app.container.pairingStore.load())
    }
}
