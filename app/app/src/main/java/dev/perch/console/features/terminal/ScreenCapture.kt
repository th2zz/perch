package dev.perch.console.features.terminal

import android.app.Activity
import android.graphics.Bitmap
import android.os.Handler
import android.os.Looper
import android.view.PixelCopy
import java.io.ByteArrayOutputStream
import kotlin.coroutines.resume
import kotlinx.coroutines.suspendCancellableCoroutine

/**
 * A picture of what is actually on the screen, taken because the user asked for one.
 *
 * `PixelCopy` of the window rather than `View.draw` of the WebView, and the difference is the whole point:
 * this product spent two days on a terminal that was black while every view and page reported itself
 * healthy, and the only person who could see the pixels was describing them in words. A window copy is
 * *the pixels* — composited, after every layout and paint decision, including any view that is drawn over
 * the terminal or not drawn at all. `View.draw` would have been the WebView's own idea of itself, which is
 * exactly the thing that was lying.
 *
 * Nothing calls this on a timer. It is wired to a button that says what it does.
 */
object ScreenCapture {

    data class Stats(
        val width: Int,
        val height: Int,
        val sampled: Int,
        /** The fraction of sampled pixels that are the most common colour, in per mille. */
        val dominantPermille: Int,
        val dominantColour: Int,
        val distinctColours: Int,
    )

    /** One line, for the computer's log. */
    fun describe(stats: Stats): String {
        val percent = stats.dominantPermille / 10.0
        return "pixels: ${stats.width}x${stats.height}, ${stats.distinctColours} colours, " +
            "most common #${"%08X".format(stats.dominantColour)} $percent% of ${stats.sampled} sampled"
    }

    /** Where the pixels stand, sampled every [stride] pixels in each direction to keep this cheap. */
    suspend fun windowStats(activity: Activity, stride: Int = 3): Stats? {
        val bitmap = windowBitmap(activity) ?: return null
        return try {
            val counts = HashMap<Int, Int>()
            var sampled = 0
            var y = 0
            while (y < bitmap.height) {
                var x = 0
                while (x < bitmap.width) {
                    val colour = bitmap.getPixel(x, y)
                    counts[colour] = (counts[colour] ?: 0) + 1
                    sampled++
                    x += stride
                }
                y += stride
            }
            val top = counts.maxByOrNull { it.value } ?: return null
            Stats(
                width = bitmap.width,
                height = bitmap.height,
                sampled = sampled,
                dominantPermille = if (sampled == 0) 0 else top.value * 1000 / sampled,
                dominantColour = top.key,
                distinctColours = counts.size,
            )
        } finally {
            bitmap.recycle()
        }
    }

    /** The window as a PNG, or null when the platform will not give us one. */
    suspend fun windowPng(activity: Activity): ByteArray? {
        val bitmap = windowBitmap(activity) ?: return null
        return ByteArrayOutputStream().use { out ->
            bitmap.compress(Bitmap.CompressFormat.PNG, 100, out)
            bitmap.recycle()
            out.toByteArray()
        }
    }

    private suspend fun windowBitmap(activity: Activity): Bitmap? {
        val window = activity.window ?: return null
        val width = window.decorView.width
        val height = window.decorView.height
        if (width <= 0 || height <= 0) return null

        val bitmap = Bitmap.createBitmap(width, height, Bitmap.Config.ARGB_8888)
        val handler = Handler(Looper.getMainLooper())
        val result = suspendCancellableCoroutine { continuation ->
            try {
                PixelCopy.request(window, bitmap, { code ->
                    if (code == PixelCopy.SUCCESS) {
                        continuation.resume(bitmap)
                    } else {
                        bitmap.recycle()
                        continuation.resume(null)
                    }
                }, handler)
            } catch (e: Exception) {
                // A window that cannot be copied is not a reason for the screen to break: this exists to
                // explain a failure, never to add one.
                bitmap.recycle()
                continuation.resume(null)
            }
        }
        return result
    }
}
