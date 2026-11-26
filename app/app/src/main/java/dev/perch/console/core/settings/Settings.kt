package dev.perch.console.core.settings

import android.content.Context
import androidx.core.content.edit
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

/**
 * The handful of preferences the app has.
 *
 * Two, and both are real choices rather than settings for their own sake: whether the phone keeps a
 * connection open to the computer so decisions arrive while the app is closed, and which working
 * folders the person has used. The first has a visible cost (one quiet notification, some battery),
 * so the user decides it, and the default after pairing is on — the product is useless without it.
 * The second exists so the folder picker starts where the person last worked instead of at home.
 */
class Settings(context: Context) {

    private val prefs = context.applicationContext
        .getSharedPreferences("perch.settings", Context.MODE_PRIVATE)
    private val json = Json { ignoreUnknownKeys = true }

    var watchInBackground: Boolean
        get() = prefs.getBoolean(KEY_WATCH, true)
        set(value) = prefs.edit { putBoolean(KEY_WATCH, value) }

    /**
     * Working folders this phone has created a project in, most recent first.
     *
     * These are paths on the paired computer, so they are cleared when the pairing is forgotten:
     * offering one computer's folders while looking at another would be a list of places that do
     * not exist. JSON rather than a delimiter: a folder name may contain any character a filesystem
     * allows, and a separator that assumes otherwise would split one path into two rows.
     */
    val recentFolders: List<String>
        get() = runCatching {
            json.decodeFromString<List<String>>(prefs.getString(KEY_RECENT, null) ?: "[]")
        }.getOrDefault(emptyList())

    /** Records [path] as the last folder used, keeping the list short and free of duplicates. */
    fun rememberFolder(path: String) {
        val clean = path.trim()
        if (clean.isEmpty()) return
        val next = (listOf(clean) + recentFolders.filterNot { it == clean })
            .take(MAX_RECENT_FOLDERS)
        prefs.edit { putString(KEY_RECENT, json.encodeToString(next)) }
    }

    /** Forgets every remembered folder; called when the phone forgets its computer. */
    fun clearRecentFolders() {
        prefs.edit { remove(KEY_RECENT) }
    }

    private companion object {
        const val KEY_WATCH = "watch_in_background"
        const val KEY_RECENT = "recent_folders"
        const val MAX_RECENT_FOLDERS = 8
    }
}
