package dev.perch.console

import java.io.File
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The words the product shows.
 *
 * Two promises are checkable by reading the copy, and both rot quietly:
 *
 *  - SC-005 caps the vocabulary the user has to learn. The designed path
 *    introduces none, so a term leaking into a string is a regression rather than
 *    a judgment call.
 *  - SC-013 counts "please edit this file" style advice as a defect. This audience
 *    can edit files; the point is that needing to is a failure of the product.
 *
 * A test rather than a lint rule because the failure mode is a sentence someone
 * wrote in good faith, and the guard has to live where the sentences are.
 */
class CopyTest {

    private val stringsXml: File = listOf(
        File("src/main/res/values/strings.xml"),
        File("app/src/main/res/values/strings.xml"),
    ).firstOrNull { it.exists() }
        ?: error("could not find strings.xml; this test must not pass by looking in the wrong place")

    private val strings: List<Pair<String, String>> by lazy {
        val pattern = Regex("""<string name="([^"]+)"[^>]*>(.*?)</string>""", RegexOption.DOT_MATCHES_ALL)
        pattern.findAll(stringsXml.readText())
            .map { it.groupValues[1] to it.groupValues[2] }
            .toList()
    }

    @Test
    fun theStringsFileIsActuallyBeingRead() {
        assertTrue("no strings were parsed; the pattern or the path is wrong", strings.size > 50)
    }

    @Test
    fun noTermsTheUserWouldHaveToLearn() {
        // Words about how the product is built, not about what the user is doing.
        val forbidden = listOf(
            "herdr", "tmux", "psmux", "multiplexer", "ssh", "mosh", "socket", "daemon",
            "systemd", "launchd", "config.toml", "manifest", "endpoint", "registry",
            "bracketed", "alt-screen", "json", "api", "cli", "repo",
        )
        val offenders = mutableListOf<String>()
        for ((name, text) in strings) {
            // The pairing link prefix is a literal the user copies, not vocabulary.
            val haystack = text.lowercase().replace("perch://", "")
            for (word in forbidden) {
                if (Regex("\\b" + Regex.escape(word)).containsMatchIn(haystack)) {
                    offenders += "$name: $word"
                }
            }
        }
        assertTrue(
            "these strings ask the user to know how the product works:\n  " +
                offenders.joinToString("\n  "),
            offenders.isEmpty(),
        )
    }

    @Test
    fun nothingAsksTheUserToEditAFile() {
        val offenders = strings.filter { (_, text) ->
            val lower = text.lowercase()
            listOf("edit the", "edit your", "modify the", ".toml", ".yaml", ".yml", ".json file")
                .any { it in lower }
        }
        assertTrue(
            "an instruction to edit a file means the product failed:\n  " +
                offenders.joinToString("\n  "),
            offenders.isEmpty(),
        )
    }

    @Test
    fun everyFailureStringIsPairedWithANextStep() {
        // FR-015: a reason and a next step, always. The naming convention is what
        // makes this checkable, so the convention is the contract.
        val failures = strings.filter { (name, _) -> name.startsWith("failure_") }
        assertTrue("failure copy should exist", failures.isNotEmpty())

        val withFix = failures.filter { (name, _) -> name.endsWith("_fix") }.map { it.first }
        assertTrue("every failure reason needs a _fix twin", withFix.isNotEmpty())

        for ((name, text) in failures) {
            assertTrue("$name is empty", text.isNotBlank())
            if (!name.endsWith("_fix")) {
                val twin = "${name}_fix"
                assertTrue(
                    "$name has no $twin, so the user is told what went wrong and not what to do",
                    strings.any { it.first == twin },
                )
            }
        }
    }
}
