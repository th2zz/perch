package dev.perch.console

import dev.perch.console.core.auth.PairLink
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The app's half of the pairing link.
 *
 * A plain JVM test, because the parsing deliberately uses java.net.URI rather
 * than android.net.Uri: the rules here are RFC 3986, and keeping the framework
 * out means this test runs the real code instead of a robotised approximation.
 */
class PairLinkTest {

    @Test
    fun readsTheLinkTheComputerPrints() {
        // Exactly what `perch-bridge pair --link` prints, with a real token shape.
        val raw = "perch://pair?address=100.64.0.1%3A45999" +
            "&name=dev-laptop&token=lVVUUr4n1fnQe81UpIA5zCOrBRM9JgvKOf623GsOc2U"

        val invite = PairLink.parse(raw)
        assertTrue("the link must parse", invite != null)
        assertEquals("100.64.0.1:45999", invite!!.address)
        assertEquals("lVVUUr4n1fnQe81UpIA5zCOrBRM9JgvKOf623GsOc2U", invite.token)
        assertEquals("dev-laptop", invite.computerName)
    }

    @Test
    fun aMissingNameIsFineBecauseItIsOnlyALabel() {
        val invite = PairLink.parse("perch://pair?address=my-box:45999&token=abc")
        assertTrue(invite != null)
        assertEquals("", invite!!.computerName)
    }

    @Test
    fun refusesAnythingThatCouldPointTheAppElsewhere() {
        val bad = listOf(
            null,
            "",
            "   ",
            "https://example.com/?address=a&token=b",
            "perch://elsewhere?address=a&token=b",
            "perch://pair?address=only-an-address",
            "perch://pair?token=only-a-token",
            "just some text the user pasted",
        )
        bad.forEach { assertNull("must reject: $it", PairLink.parse(it)) }
    }

    @Test
    fun recognisesAPairingLinkBeforeTryingToParseIt() {
        // The activity uses this to decide whether an incoming intent is ours.
        assertTrue(PairLink.looksLikeInvite("perch://pair?address=a&token=b"))
        assertTrue(PairLink.looksLikeInvite("  perch://PAIR?address=a&token=b  "))
        assertFalse(PairLink.looksLikeInvite("https://example.com"))
        assertFalse(PairLink.looksLikeInvite(null))
    }

    @Test
    fun aLinkPastedIntoEitherFieldIsUnderstood() {
        val link = "perch://pair?address=100.64.0.1%3A45999&token=abc123"

        // Pasted into the address field, as a user would do first.
        val fromAddress = PairLink.fromFields(link, "")
        assertTrue(fromAddress != null)
        assertEquals("100.64.0.1:45999", fromAddress!!.address)
        assertEquals("abc123", fromAddress.token)

        // Or into the token field, which is where a hasty paste often lands.
        val fromToken = PairLink.fromFields("", link)
        assertTrue(fromToken != null)
        assertEquals("100.64.0.1:45999", fromToken!!.address)

        // Ordinary typed-in values are untouched by this.
        assertNull(PairLink.fromFields("100.64.0.1:45999", "abc123"))
    }
}
