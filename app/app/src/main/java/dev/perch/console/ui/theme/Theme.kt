package dev.perch.console.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

/**
 * The app follows the user's own theme.
 *
 * No brand palette is imposed: this is a tool that sits next to a terminal, and
 * a tool that fights the user's system settings is a tool they turn off. On
 * Android 12 and up the palette is taken from the wallpaper, which is what the
 * rest of the phone already looks like.
 */
@Composable
fun PerchTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColour: Boolean = true,
    content: @Composable () -> Unit,
) {
    // Dynamic colour needs Android 12 (API 31); the minimum supported version is
    // 33, so the only thing left to check is whether the user wants it.
    val scheme = when {
        dynamicColour -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        darkTheme -> darkColorScheme()
        else -> lightColorScheme()
    }
    MaterialTheme(colorScheme = scheme, content = content)
}
