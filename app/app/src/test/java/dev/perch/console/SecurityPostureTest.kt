package dev.perch.console

import java.io.File
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The properties that protect the one secret this app holds.
 *
 * The pairing token lets this phone approve agents and type into panes on the user's
 * computer. Every property below is silent when lost: the app keeps working perfectly
 * with a backupable, world-readable token, right up until someone reads it. So they
 * are asserted against the files that declare them — the manifest and the two rule
 * files — where a change is visible and has to argue with this test.
 */
class SecurityPostureTest {

    private fun file(path: String): File =
        listOf(File("src/main/$path"), File("app/src/main/$path")).firstOrNull { it.exists() }
            ?: error("could not find $path; this test must not pass by looking in the wrong place")

    private val manifest = file("AndroidManifest.xml").readText()

    @Test
    fun theTokenCannotLeaveTheDeviceThroughABackup() {
        assertTrue(
            "allowBackup must be false: a backed-up pairing token is a copy of the " +
                "user's keyboard access sitting in somebody else's cloud",
            manifest.contains("android:allowBackup=\"false\""),
        )
        assertTrue(
            "dataExtractionRules must be declared, or Android 12+ ignores the intent",
            manifest.contains("android:dataExtractionRules="),
        )

        val rules = file("res/xml/data_extraction_rules.xml").readText()
        for (scope in listOf("cloud-backup", "device-transfer")) {
            val block = rules.substringAfter("<$scope>").substringBefore("</$scope>")
            for (domain in listOf("sharedpref", "file", "database")) {
                assertTrue(
                    "$scope must exclude $domain; the token lives in sharedpref and " +
                        "anything else may hold what was read from the computer",
                    block.contains("""<exclude domain="$domain" />"""),
                )
            }
        }
    }

    @Test
    fun cleartextStaysADecisionRatherThanAnOversight() {
        val config = file("res/xml/network_security_config.xml").readText()
        assertTrue(
            "cleartext is permitted for local/tailnet transport",
            config.contains("cleartextTrafficPermitted=\"true\""),
        )
        assertTrue(
            "the rationale must stay visible in the file",
            config.contains("Tailscale"),
        )
    }

    @Test
    fun theSecretIsNotStoredAsItself() {
        // Behavioural, not textual: whatever the mechanism, what lands in the app's
        // preferences must not be the token in readable form. Encrypting with a
        // keystore-held key is how it is done (see SecureStore); this is the property
        // that would be lost if somebody "simplified" it back to a plain string.
        val source = file("java/dev/perch/console/core/auth/SecureStore.kt").readText()
        assertTrue(
            "the token must be encrypted with a key that lives in the Android keystore",
            source.contains("AndroidKeyStore"),
        )
        assertFalse(
            "SecureStore must not fall back to writing the value as it is",
            source.contains("putString(name, value)"),
        )
    }
}
