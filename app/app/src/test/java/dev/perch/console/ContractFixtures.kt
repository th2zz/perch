package dev.perch.console

import java.io.File

/**
 * Reads the shared contract files the bridge writes.
 *
 * Four tests now check a different half of the same wire — the pairing link's
 * bytes, the event type names, the JSON field names, the values inside them — and
 * each of them needs the same two things: the checkout and the file. Written once
 * here so that a change to how the path is resolved cannot make one of them look
 * in a different place from the others, and so "the file is missing" is one
 * failure with one message rather than four chances to get it subtly wrong.
 *
 * The checkout is found by walking up to `.git` rather than by a configured path:
 * Gradle runs unit tests with the module directory as the working directory, and
 * pinning `../../..` would break the moment that changes. A missing file is a
 * **failure**, never a skip — a contract test that quietly does nothing reports
 * green for the one thing it exists to catch.
 */
object ContractFixtures {

    private val root: File by lazy {
        var dir: File? = File(System.getProperty("user.dir")!!).absoluteFile
        while (dir != null && !File(dir, ".git").exists()) dir = dir.parentFile
        dir ?: error("could not find the repository root above ${System.getProperty("user.dir")} (no .git)")
    }

    /** The raw text of a repo-relative contract file, failing with the path and a hint. */
    fun text(relativePath: String, regenerateHint: String): String {
        val file = File(root, relativePath)
        check(file.isFile) {
            "the shared contract file is missing: ${file.path}. Regenerate it with: $regenerateHint"
        }
        return file.readText()
    }
}
