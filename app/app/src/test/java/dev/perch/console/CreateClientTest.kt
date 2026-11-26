package dev.perch.console

import com.sun.net.httpserver.HttpExchange
import com.sun.net.httpserver.HttpServer
import dev.perch.console.core.herdrclient.BridgeClient
import java.net.InetSocketAddress
import java.net.URLDecoder
import java.nio.charset.StandardCharsets
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import kotlinx.coroutines.runBlocking
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.OkHttpClient
import org.junit.After
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35], qualifiers = "w411dp-h891dp-xhdpi")
class CreateClientTest {

    private lateinit var server: HttpServer
    private lateinit var client: BridgeClient
    private val bodies = ConcurrentHashMap<String, String>()
    private val queries = ConcurrentHashMap<String, String>()

    @Before
    fun start() {
        server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0).apply {
            createContext("/v1/projects") { exchange -> capture(exchange, "projects") }
            createContext("/v1/sessions") { exchange -> capture(exchange, "sessions") }
            createContext("/v1/folders") { exchange ->
                queries["folders"] = exchange.requestURI.rawQuery.orEmpty()
                val reply =
                    """{"path":"/home/me/work","parent":"/home/me","home":"/home/me","entries":[{"name":"one","path":"/home/me/work/one"}],"truncated":false}"""
                        .toByteArray()
                exchange.responseHeaders.add("Content-Type", "application/json")
                exchange.sendResponseHeaders(200, reply.size.toLong())
                exchange.responseBody.use { it.write(reply) }
            }
            executor = Executors.newCachedThreadPool()
            start()
        }
        client = BridgeClient("http://127.0.0.1:${server.address.port}".toHttpUrl(), "token")
    }

    @After
    fun stop() {
        server.stop(0)
    }

    @Test
    fun createProjectSendsArgumentsAsAnArray() = runBlocking {
        val created = client.createProject(
            label = "my project",
            cwd = "/tmp",
            kind = "codex",
            name = "one",
            args = listOf("--model", "gpt 5"),
        )
        assertEquals("w1", created.workspaceId)
        assertEquals("w1:p1", created.paneId)

        val body = Json.parseToJsonElement(bodies["projects"] ?: error("no project body")).jsonObject
        assertEquals("my project", body["label"]?.jsonPrimitive?.content)
        assertEquals("codex", body["kind"]?.jsonPrimitive?.content)
        // The folder the person chose is what the bridge must create the project in; an omitted cwd
        // used to mean "home", which is the bug the picker exists to close.
        assertEquals("/tmp", body["cwd"]?.jsonPrimitive?.content)
        assertEquals(
            listOf("--model", "gpt 5"),
            body["args"]?.jsonArray?.map { it.jsonPrimitive.content },
        )
    }

    @Test
    fun foldersDecodeTheListingAndSendThePathAsAQuery() = runBlocking {
        val listing = client.folders("/home/me/work")

        assertEquals("/home/me/work", listing.path)
        assertEquals("/home/me", listing.parent)
        assertEquals("/home/me", listing.home)
        assertEquals("/home/me/work/one", listing.entries.single().path)
        val query = URLDecoder.decode(
            queries["folders"] ?: error("no folder query"),
            StandardCharsets.UTF_8.name(),
        )
        assertEquals("path=/home/me/work", query)
    }

    @Test
    fun createSessionNamesTheWorkspaceAndHarness() = runBlocking {
        val created = client.createSession(
            workspaceId = "w1",
            kind = "claude",
            name = "two",
            args = listOf("--resume"),
        )
        assertEquals("w1:t2", created.tabId)

        val body = Json.parseToJsonElement(bodies["sessions"] ?: error("no session body")).jsonObject
        assertEquals("w1", body["workspace_id"]?.jsonPrimitive?.content)
        assertEquals("claude", body["kind"]?.jsonPrimitive?.content)
        assertEquals(listOf("--resume"), body["args"]?.jsonArray?.map { it.jsonPrimitive.content })
    }

    @Test
    fun creationOutlivesTheScreensShortCallTimeout() = runBlocking {
        val slow = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0).apply {
            createContext("/v1/projects") { exchange ->
                exchange.requestBody.readBytes()
                Thread.sleep(400)
                val reply =
                    """{"workspace_id":"w9","tab_id":"w9:t1","pane_id":"w9:p1","equivalent":"x"}"""
                        .toByteArray()
                exchange.responseHeaders.add("Content-Type", "application/json")
                exchange.sendResponseHeaders(200, reply.size.toLong())
                exchange.responseBody.use { it.write(reply) }
            }
            executor = Executors.newCachedThreadPool()
            start()
        }
        try {
            val short = OkHttpClient.Builder()
                .callTimeout(150, TimeUnit.MILLISECONDS)
                .build()
            val patient = BridgeClient(
                "http://127.0.0.1:${slow.address.port}".toHttpUrl(),
                "token",
                short,
            )

            val created = patient.createProject(label = "demo", cwd = "/tmp", kind = "codex")
            assertEquals("w9", created.workspaceId)
        } finally {
            slow.stop(0)
        }
    }

    private fun capture(exchange: HttpExchange, key: String) {
        bodies[key] = exchange.requestBody.readBytes().decodeToString()
        val reply = if (key == "projects") {
            """{"workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","equivalent":"x"}"""
        } else {
            """{"workspace_id":"w1","tab_id":"w1:t2","pane_id":"w1:p2","equivalent":"y"}"""
        }.toByteArray()
        exchange.responseHeaders.add("Content-Type", "application/json")
        exchange.sendResponseHeaders(200, reply.size.toLong())
        exchange.responseBody.use { it.write(reply) }
    }
}
