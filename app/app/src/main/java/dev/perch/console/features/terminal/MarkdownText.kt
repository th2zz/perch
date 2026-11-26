package dev.perch.console.features.terminal

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.IntrinsicSize
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontStyle
import androidx.compose.ui.text.font.FontWeight
import dev.perch.console.core.format.Markdown
import androidx.compose.ui.unit.dp

/**
 * The escape hatch's text, rendered.
 *
 * [Markdown] does the reading — it is a pure function with its own tests, because
 * the parsing is where the mistakes would be. This file only decides what each
 * block looks like, and it is deliberately plain: the reader is looking at what an
 * agent said, not at a document, and every extra flourish is space a phone does not
 * have.
 *
 * The raw text is never thrown away: the screen keeps a Rendered/Raw switch, because
 * a terminal's own layout is sometimes the thing being read (an aligned table, a
 * box, a stack trace with a pointing caret) and a renderer that guessed wrong there
 * would be hiding the answer.
 */
@Composable
fun MarkdownText(
    text: String,
    modifier: Modifier = Modifier,
    // The base for everything that is not a heading: the terminal view wants
    // bodyMedium (it is the text that screen exists to show), a card wants
    // bodySmall (it sits inside one). Headings still come from the theme, because
    // "bigger than the body" is the whole of what a heading means.
    style: TextStyle = MaterialTheme.typography.bodyMedium,
) {
    val blocks = remember(text) { Markdown.parse(text) }
    Column(modifier, verticalArrangement = Arrangement.spacedBy(6.dp)) {
        for (block in blocks) {
            when (block) {
                is Markdown.Block.Heading -> InlineMarkdown(
                    text = block.text,
                    style = when (block.level) {
                        1 -> MaterialTheme.typography.titleLarge
                        2 -> MaterialTheme.typography.titleMedium
                        else -> MaterialTheme.typography.titleSmall
                    },
                    modifier = Modifier.padding(top = 2.dp),
                )

                is Markdown.Block.Paragraph -> InlineMarkdown(
                    text = block.text,
                    // bodyMedium, not bodySmall: this is the text the screen exists
                    // to show, read in portrait. The same choice the raw view makes.
                    style = MaterialTheme.typography.bodyMedium,
                )

                is Markdown.Block.Bullet -> Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text("•", style = style)
                    InlineMarkdown(block.text, style)
                }

                is Markdown.Block.Numbered -> Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text(block.marker, style = style)
                    InlineMarkdown(block.text, style)
                }

                is Markdown.Block.Quote -> Row(
                    // IntrinsicSize.Min so the bar beside the text can be as tall as
                    // the text: a Row measures children at their own height, and a
                    // bar with no content of its own would come out zero.
                    modifier = Modifier.fillMaxWidth().height(IntrinsicSize.Min),
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                ) {
                    // A bar rather than quotation marks: it reads as "this came from
                    // somewhere else" without adding punctuation the agent did not
                    // write.
                    Box(
                        modifier = Modifier
                            .width(3.dp)
                            .fillMaxHeight()
                            .background(MaterialTheme.colorScheme.outlineVariant),
                    )
                    InlineMarkdown(block.text, style)
                }

                is Markdown.Block.Code -> CodeBlock(block, style)

                is Markdown.Block.Table -> TableBlock(block)

                Markdown.Block.Rule -> HorizontalDivider()
            }
        }
    }
}

@Composable
private fun CodeBlock(block: Markdown.Block.Code, style: TextStyle) {
    Text(
        text = block.text,
        style = style,
        fontFamily = FontFamily.Monospace,
        modifier = Modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceVariant)
            .padding(8.dp),
    )
}

@Composable
private fun TableBlock(table: Markdown.Block.Table) {
    val labelColour: Color = MaterialTheme.colorScheme.onSurfaceVariant
    val labelsNeeded = table.rows.any { it.size > 2 }
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(vertical = 4.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        table.rows.forEachIndexed { index, row ->
            if (index > 0) HorizontalDivider()
            Column(verticalArrangement = Arrangement.spacedBy(2.dp)) {
                val lead = row.firstOrNull().orEmpty()
                if (lead.isNotBlank()) {
                    InlineMarkdown(
                        text = lead,
                        style = MaterialTheme.typography.bodyMedium
                            .copy(fontWeight = FontWeight.Bold),
                    )
                }
                for (column in 1 until row.size) {
                    val value = row[column]
                    if (value.isBlank()) continue
                    val label = table.header.getOrNull(column).orEmpty()
                    Row(modifier = Modifier.fillMaxWidth()) {
                        if (label.isNotBlank() && labelsNeeded) {
                            Text(
                                text = "$label  ",
                                style = MaterialTheme.typography.labelMedium,
                                color = labelColour,
                            )
                        }
                        // In its own Text so a long value wraps under the label
                        // instead of stretching the row.
                        InlineMarkdown(
                            text = value,
                            style = MaterialTheme.typography.bodyMedium,
                            modifier = Modifier.weight(1f),
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun InlineMarkdown(text: String, style: TextStyle, modifier: Modifier = Modifier) {
    val (plain, spans) = remember(text) { Markdown.inline(text) }
    val codeColour: Color = MaterialTheme.colorScheme.primary
    fun spanStyle(emphasis: Markdown.Emphasis): SpanStyle = when (emphasis) {
        Markdown.Emphasis.Bold -> SpanStyle(fontWeight = FontWeight.Bold)
        Markdown.Emphasis.Italic -> SpanStyle(fontStyle = FontStyle.Italic)
        Markdown.Emphasis.Code -> SpanStyle(
            fontFamily = FontFamily.Monospace,
            color = codeColour,
        )
    }
    Text(
        text = buildAnnotatedString {
            // The marker-free text goes in first, then the spans are applied by
            // range: a span's offsets are into exactly this text, which is why
            // Markdown.inline returns the pair together.
            append(plain)
            for (span in spans) {
                addStyle(spanStyle(span.emphasis), span.start, span.end)
            }
        },
        style = style,
        modifier = modifier,
    )
}
