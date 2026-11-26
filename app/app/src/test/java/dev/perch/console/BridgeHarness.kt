package dev.perch.console

import java.io.File
import java.nio.file.Files
import java.util.concurrent.TimeUnit
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrl

/**
 * Starts the real bridge for the integration tests.
 *
 * This is the seam where the product is most likely to break and least likely to
 * be noticed: the app and the bridge are separate programs in separate
 * languages, and a unit test on either side can be perfectly green while the
 * pair is incompatible. So the tests that matter drive the actual binary with
 * the actual client code.
 *
 * Everything it needs is either discovered or skipped: if the binary was not
 * built (no Go toolchain) the tests skip, and if herdr is not running the
 * read-only tests skip. Nothing here reaches for the developer's real session —
 * the bridge gets its own state directory and a port the OS picks.
 */
object BridgeHarness {

    /** True when a bridge was started and is answering. */
    @Volatile
    var available = false
        private set

    /** True when herdr is running, so read-only endpoints can succeed. */
    @Volatile
    var herdrRunning = false
        private set

    lateinit var baseUrl: HttpUrl
        private set

    lateinit var token: String
        private set

    private var process: Process? = null
    private var stateDir: File? = null
    private var started = false

    @Synchronized
    fun start() {
        if (started) return
        started = true

        herdrRunning = commandSucceeds("herdr", "status")

        val binary = System.getProperty("perch.bridge.bin")
            ?.let(::File)
            ?.takeIf { it.canExecute() }
            ?: return

        val dir = Files.createTempDirectory("perch-integration-").toFile()
        stateDir = dir
        val log = File(dir, "bridge.log")

        val builder = ProcessBuilder(binary.absolutePath, "run")
        builder.environment()["HERDR_PLUGIN_STATE_DIR"] = dir.absolutePath
        // Port 0: let the OS pick, so two test runs cannot collide with each
        // other or with a bridge the developer is actually using.
        builder.environment()["PERCH_BRIDGE_ADDR"] = "127.0.0.1:0"
        builder.redirectErrorStream(true)
        builder.redirectOutput(log)

        val started = builder.start()
        process = started
        Runtime.getRuntime().addShutdownHook(Thread { started.destroy() })

        val address = awaitListeningAddress(log, started) ?: run {
            started.destroy()
            return
        }
        val pairing = File(dir, "pairing.json")
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(10)
        while (!pairing.exists() && System.nanoTime() < deadline) {
            Thread.sleep(50)
        }
        if (!pairing.exists()) {
            started.destroy()
            return
        }

        baseUrl = "http://$address".toHttpUrl()
        token = Regex("\"token\"\\s*:\\s*\"([^\"]+)\"")
            .find(pairing.readText())
            ?.groupValues
            ?.get(1)
            ?: run {
                started.destroy()
                return
            }
        available = true
    }

    fun stop() {
        process?.destroy()
        process?.waitFor(5, TimeUnit.SECONDS)
        process = null
        stateDir?.deleteRecursively()
        stateDir = null
        available = false
        started = false
    }

    /** Reads the address the bridge actually bound, from its own log line. */
    private fun awaitListeningAddress(log: File, process: Process): String? {
        val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(20)
        while (System.nanoTime() < deadline) {
            if (!process.isAlive && log.length() == 0L) return null
            val match = Regex("""addr=([0-9.]+:\d+)""").find(log.readText())
            if (match != null) return match.groupValues[1]
            Thread.sleep(100)
        }
        return null
    }

    private fun commandSucceeds(vararg argv: String): Boolean = runCatching {
        // Output goes to a scratch file rather than the console: the unit test
        // report is not the place for another program's banner, and
        // ProcessBuilder.Redirect.DISCARD is not on the Android API level this
        // module compiles against.
        val scratch = File.createTempFile("perch-probe-", ".log")
        scratch.deleteOnExit()
        val process = ProcessBuilder(*argv)
            .redirectErrorStream(true)
            .redirectOutput(scratch)
            .start()
        process.waitFor(10, TimeUnit.SECONDS) && process.exitValue() == 0
    }.getOrDefault(false)
}
