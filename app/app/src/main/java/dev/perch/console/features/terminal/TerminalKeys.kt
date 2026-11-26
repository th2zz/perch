package dev.perch.console.features.terminal

object TerminalKeys {

    /** One key: what it is called on screen, and the bytes it types. */
    data class Key(val label: String, val bytes: ByteArray)

    val all: List<Key> = listOf(
        Key("esc", byteArrayOf(0x1b)),
        Key("enter", byteArrayOf(0x0d)),
        Key("tab", byteArrayOf(0x09)),
        Key("↑", byteArrayOf(0x1b, '['.code.toByte(), 'A'.code.toByte())),
        Key("↓", byteArrayOf(0x1b, '['.code.toByte(), 'B'.code.toByte())),
        Key("←", byteArrayOf(0x1b, '['.code.toByte(), 'D'.code.toByte())),
        Key("→", byteArrayOf(0x1b, '['.code.toByte(), 'C'.code.toByte())),
        Key("ctrl+c", byteArrayOf(0x03)),
        Key("y", byteArrayOf('y'.code.toByte())),
        Key("n", byteArrayOf('n'.code.toByte())),
        Key("1", byteArrayOf('1'.code.toByte())),
        Key("2", byteArrayOf('2'.code.toByte())),
    )
}

