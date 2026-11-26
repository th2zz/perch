package dev.perch.console.features.terminal

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.unit.sp

@Composable
fun ScreenText(
    text: String,
    modifier: Modifier = Modifier,
    colour: Color = MaterialTheme.colorScheme.onSurface,
) {
    // One Text, not a line per row: the text already has the newlines, and a single node keeps selection
    // and copy working across the whole screen (copying an error out of it is the most common reason this
    // screen is open at all).
    Text(
        text = text,
        modifier = modifier
            .fillMaxWidth()
            // Sideways, not folded: a wrapped screen stops being the screen somebody is looking at.
            .horizontalScroll(rememberScrollState()),
        fontFamily = FontFamily.Monospace,
        fontSize = 12.sp,
        lineHeight = 16.sp,
        softWrap = false,
        color = colour,
    )
}
