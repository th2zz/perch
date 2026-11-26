package dev.perch.console

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import dev.perch.console.core.voice.Transcriber
import dev.perch.console.core.voice.Transcription
import dev.perch.console.core.voice.VoiceConfig
import java.io.File
import java.net.InetSocketAddress
import java.util.concurrent.CopyOnWriteArrayList
import kotlinx.coroutines.runBlocking
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The audio path, against a provider that does not exist.
 *
 * What can be checked here is everything except the provider's own behaviour: does
 * the app post the file to the right place, with the key and the fields the
 * convention requires, and does it turn each kind of answer into something the user
 * can act on. The provider's side is their contract; ours is this.
 *
 * The rule from spec FR-014 that this test exists to protect is the negative one:
 * with no key configured there must be no request at all. A microphone that
 * silently uploads to a default account would be the worst version of this feature.
 */
class TranscriberTest {

    private val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
    private val requests = CopyOnWriteArrayList<CapturedRequest>()
    private var responder: (HttpExchange) -> Pair<Int, String> = { 200 to """{"text":"ok"}""" }

    private data class CapturedRequest(
        val path: String,
        val authorization: String?,
        val contentType: String?,
        val body: String,
    )

    private fun endpoint(path: String = "/v1/audio/transcriptions"): String {
        server.createContext(path) { exchange ->
            val body = exchange.requestBody.readBytes().toString(Charsets.ISO_8859_1)
            requests += CapturedRequest(
                path = exchange.requestURI.path,
                authorization = exchange.requestHeaders.getFirst("Authorization"),
                contentType = exchange.requestHeaders.getFirst("Content-Type"),
                body = body,
            )
            val (status, payload) = responder(exchange)
            val bytes = payload.toByteArray()
            exchange.sendResponseHeaders(status, bytes.size.toLong())
            exchange.responseBody.use { it.write(bytes) }
        }
        server.start()
        return "http://127.0.0.1:${server.address.port}$path"
    }

    @After
    fun stop() {
        runCatching { server.stop(0) }
    }

    private fun recording(bytes: Int = 2048): File =
        File.createTempFile("perch-voice-", ".m4a").apply {
            writeBytes(ByteArray(bytes) { 1 })
            deleteOnExit()
        }

    @Test
    fun `the recording is posted where the user pointed and nowhere else`() {
        val url = endpoint()
        responder = { 200 to """{"text":"rename the handler function in café.ts" }""" }

        val result = runBlocking {
            Transcriber().transcribe(
                recording(),
                VoiceConfig(endpoint = url, apiKey = "sk-user-supplied", model = "whisper-1"),
            )
        }

        assertEquals(Transcription.Text("rename the handler function in café.ts"), result)

        assertEquals(1, requests.size)
        val request = requests.first()
        assertEquals("/v1/audio/transcriptions", request.path)
        assertEquals("Bearer sk-user-supplied", request.authorization)
        assertTrue(
            "the request must be multipart: ${request.contentType}",
            request.contentType?.startsWith("multipart/form-data") == true,
        )
        // The two fields the convention requires, plus the file itself.
        assertTrue(request.body.contains("name=\"model\""))
        assertTrue(request.body.contains("whisper-1"))
        assertTrue(request.body.contains("name=\"file\""))
        assertTrue("the audio must actually be in the body", request.body.contains("perch-voice-"))
    }

    @Test
    fun `no key means no request at all`() {
        val url = endpoint()

        val result = runBlocking {
            Transcriber().transcribe(recording(), VoiceConfig(endpoint = url, apiKey = "", model = "whisper-1"))
        }

        assertTrue(result is Transcription.Failed)
        assertTrue("nothing may be uploaded without a key", requests.isEmpty())
    }

    @Test
    fun `nothing recorded is not worth uploading`() {
        val url = endpoint()

        val result = runBlocking {
            Transcriber().transcribe(
                recording(bytes = 0),
                VoiceConfig(endpoint = url, apiKey = "sk-x", model = "whisper-1"),
            )
        }

        assertTrue(result is Transcription.Failed)
        assertTrue(requests.isEmpty())
    }

    @Test
    fun `a refused key says what to do about it`() {
        val url = endpoint()
        responder = { 401 to """{"error":{"message":"invalid api key"}}""" }

        val result = runBlocking {
            Transcriber().transcribe(recording(), VoiceConfig(url, "sk-wrong", "whisper-1"))
        }

        val failed = result as Transcription.Failed
        assertTrue("the reason should name the key: ${failed.reason}", failed.reason.contains("key"))
        assertTrue("the fix should point at Settings: ${failed.fix}", failed.fix.contains("Settings"))
    }

    @Test
    fun `a rate limit is temporary and says so`() {
        val url = endpoint()
        responder = { 429 to """{"error":{"message":"slow down"}}""" }

        val result = runBlocking {
            Transcriber().transcribe(recording(), VoiceConfig(url, "sk-x", "whisper-1"))
        }

        val failed = result as Transcription.Failed
        assertTrue(failed.reason.contains("rate limit"))
        assertTrue(failed.fix.contains("try again"))
    }

    @Test
    fun `an answer with no words in it is a failure, not empty text`() {
        val url = endpoint()
        responder = { 200 to """{"text":"   "}""" }

        val result = runBlocking {
            Transcriber().transcribe(recording(), VoiceConfig(url, "sk-x", "whisper-1"))
        }

        assertTrue(result is Transcription.Failed)
    }

    @Test
    fun `an unreachable provider is reported rather than thrown`() {
        // Point at a port nothing is listening on, which is what a wrong endpoint
        // looks like from the app's side.
        val result = runBlocking {
            Transcriber().transcribe(
                recording(),
                VoiceConfig("http://127.0.0.1:1/v1/audio/transcriptions", "sk-x", "whisper-1"),
            )
        }

        val failed = result as Transcription.Failed
        assertTrue(failed.fix.isNotBlank())
    }
}
