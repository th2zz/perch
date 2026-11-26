package dev.perch.console

import dev.perch.console.features.terminal.TerminalKeys
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * The keys under the real terminal, as bytes.
 *
 * A phone keyboard has letters and Enter; it does not have Escape, Tab, the arrows or Ctrl — and
 * Escape is precisely what an agent's dialog asks for ("esc to cancel"). These keys are what makes the
 * terminal answerable from a phone, and they are a fixed vocabulary rather than something derived, so
 * they are worth pinning byte by byte: `\u001b` is Escape and `\u0003` is Ctrl+C, and a terminal that
 * sends the wrong one is worse than one that sends nothing.
 *
 * The bytes match ANSI's own encodings — the same ones Termux and every other terminal app sends — so
 * that whatever the harness does with its own keyboard it does with ours.
 */
class TerminalKeysTest {

    private fun bytes(label: String): List<Int> =
        TerminalKeys.all.first { it.label == label }.bytes.map { it.toInt() and 0xFF }

    @Test
    fun theKeysAPhoneKeyboardLacksAreTheOnesADialogAsksFor() {
        assertEquals(listOf(0x1b), bytes("esc"))
        assertEquals(listOf(0x0d), bytes("enter"))
        assertEquals(listOf(0x09), bytes("tab"))
        assertEquals(listOf(0x03), bytes("ctrl+c"))
    }

    @Test
    fun arrowsAreTheAnsiSequencesAndNotTheEyeballsOnTheChip() {
        assertEquals(listOf(0x1b, '['.code, 'A'.code), bytes("↑"))
        assertEquals(listOf(0x1b, '['.code, 'B'.code), bytes("↓"))
        assertEquals(listOf(0x1b, '['.code, 'C'.code), bytes("→"))
        assertEquals(listOf(0x1b, '['.code, 'D'.code), bytes("←"))
    }

    @Test
    fun theDigitsAndLettersTypeThemselves() {
        // The two questions agents ask most are "do you want to proceed?" and "which one?" — answering
        // them with a literal character is the whole point of having them within reach.
        assertEquals(listOf('y'.code), bytes("y"))
        assertEquals(listOf('n'.code), bytes("n"))
        assertEquals(listOf('1'.code), bytes("1"))
        assertEquals(listOf('2'.code), bytes("2"))
    }

    @Test
    fun everyKeyTypesSomething() {
        assertTrue(TerminalKeys.all.isNotEmpty())
        for (key in TerminalKeys.all) {
            assertTrue("${key.label} types nothing", key.bytes.isNotEmpty())
        }
    }
}
