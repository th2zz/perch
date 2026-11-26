package dev.perch.console

import dev.perch.console.core.format.Markdown
import dev.perch.console.core.format.Markdown.Block
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class DiffTest {

    private val hisPastedExample = """
        1 +package dev.perch.console
        2 +
        3 +import android.graphics.Bitmap
        4 -import android.graphics.drawable.ColorDrawable
        @@ -12,7 +12,6 @@
    """.trimIndent()

    @Test
    fun numberedDiffLinesStayText() {
        val blocks = Markdown.parse(hisPastedExample)

        // `Block.Diff` no longer exists: the reader has no diff concept at all, which is the strongest
        // form of "this is left alone".
        assertTrue(
            "a diff line must stay a line: $blocks",
            blocks.all { it is Block.Paragraph || it is Block.Code || it is Block.Rule },
        )
        // And the lines survive as they were written, because text that is left alone is text a person
        // can still read and copy.
        val text = blocks.filterIsInstance<Block.Paragraph>().joinToString("\n") { it.text }
        assertTrue(text.contains("4 -import android.graphics.drawable.ColorDrawable"))
    }

    @Test
    fun aFencedBlockIsStillReadAsCode() {
        val blocks = Markdown.parse("```kotlin\nval x = 1\n```")

        assertEquals(1, blocks.filterIsInstance<Block.Code>().size)
        assertEquals("kotlin", blocks.filterIsInstance<Block.Code>().single().language)
    }
}
