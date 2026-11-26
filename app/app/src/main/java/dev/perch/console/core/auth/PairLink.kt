package dev.perch.console.core.auth

import java.net.URI
import java.net.URLDecoder
import java.nio.charset.StandardCharsets

/**
 * The pairing link, as the phone sees it.
 *
 * The computer prints one line; the user sends it to themselves in whatever chat
 * app they already use and taps it. That is the whole onboarding: no address to
 * read out, no token to copy, no camera permission to grant.
 *
 * The format is defined by the bridge's `pairlink` package, and the two
 * implementations are deliberately similar rather than generated: this is a
 * dozen lines of parsing, and a shared schema generator would be more moving
 * parts than the thing it generates.
 */
data class PairInvite(
    val address: String,
    val token: String,
    val computerName: String,
)

object PairLink {

    const val SCHEME = "perch"
    const val HOST = "pair"

    /**
     * Reads a pairing link.
     *
     * Strict about scheme and host so a link can never point the app at somewhere
     * else; forgiving about everything else, because the string has travelled
     * through a chat client that may have re-encoded it.
     */
    fun parse(raw: String?): PairInvite? {
        if (raw.isNullOrBlank()) return null
        // java.net.URI rather than android.net.Uri on purpose: the parsing rules
        // for this link are plain RFC 3986, and keeping the framework out means
        // this logic is testable on the JVM — a fast test that runs the real code
        // rather than a robotised approximation of it.
        val uri = runCatching { URI(raw.trim()) }.getOrNull() ?: return null
        if (!SCHEME.equals(uri.scheme, ignoreCase = true)) return null
        if (!HOST.equals(uri.host, ignoreCase = true)) return null

        val params = parseQuery(uri.rawQuery)
        val address = params["address"]?.trim().orEmpty()
        val token = params["token"]?.trim().orEmpty()
        if (address.isEmpty() || token.isEmpty()) return null

        return PairInvite(
            address = address,
            token = token,
            computerName = params["name"]?.trim().orEmpty(),
        )
    }

    /** True when this string is something the app should treat as an invite. */
    fun looksLikeInvite(raw: String?): Boolean =
        !raw.isNullOrBlank() && raw.trimStart().startsWith("$SCHEME://$HOST", ignoreCase = true)

    /**
     * Reads an invite out of the manual form's two fields.
     *
     * The designed path is to tap the link, but a link that arrives where it
     * cannot be opened — a chat app that will not make it tappable, or one that
     * sends it as plain text — must not leave the user retyping an address and a
     * token they already have. The link contains both, so recognising it in either
     * field turns a dead end into a paste.
     */
    fun fromFields(address: String, token: String): PairInvite? =
        parse(address) ?: parse(token)

    /**
     * Splits a query string, decoding percent-escapes.
     *
     * `URLDecoder` reads `+` as a space, and that is the correct reading of what
     * the bridge sends, not an accident. Go's `url.Values.Encode` writes a space
     * as `+` and a literal `+` as `%2B`, so a computer named "Home Mac" arrives
     * as `Home+Mac` and a token containing `+` arrives as `%2B`; both decode the
     * way they were encoded. Decoding with something that instead left `+` alone
     * would turn every computer name containing a space into one containing a
     * plus, and `%2B` is the only reason a real plus in a token is safe either
     * way — the exact bytes are pinned, in both directions, by the shared fixture
     * `contracts/pairlink-cases.json`
     * (`PairLinkFixtureTest` reads it; the bridge writes it).
     */
    private fun parseQuery(rawQuery: String?): Map<String, String> {
        if (rawQuery.isNullOrEmpty()) return emptyMap()
        val out = mutableMapOf<String, String>()
        for (pair in rawQuery.split('&')) {
            if (pair.isEmpty()) continue
            val index = pair.indexOf('=')
            val key = if (index < 0) pair else pair.substring(0, index)
            val value = if (index < 0) "" else pair.substring(index + 1)
            out[decode(key)] = decode(value)
        }
        return out
    }

    private fun decode(value: String): String =
        runCatching { URLDecoder.decode(value, StandardCharsets.UTF_8) }.getOrDefault(value)
}
