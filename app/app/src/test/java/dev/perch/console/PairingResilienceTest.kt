package dev.perch.console

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import dev.perch.console.core.auth.Pairing
import dev.perch.console.core.auth.PairingStore
import dev.perch.console.core.cache.MirrorCache
import java.io.File
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/**
 * What happens when the things the app remembers are gone or wrong.
 *
 * Every one of these is an ordinary event: the user restored a backup of app data onto
 * a new phone, cleared storage, or a write was interrupted. The product's rule is that
 * the computer is the source of truth and everything on the phone is disposable, so the
 * correct outcome in each case is "the app asks to pair again" — never a crash, and
 * never a half-pairing that sends a token nobody will accept.
 */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [35])
class PairingResilienceTest {

    private val context: Application = ApplicationProvider.getApplicationContext()

    private val prefsName = "perch.secure"

    // Note on the keystore: Robolectric has no `AndroidKeyStore` provider, so nothing
    // here calls `SecureStore.put`. The tests write the preferences directly, which is
    // also what they are simulating — a phone whose stored blob came from somewhere
    // else, or a state that was cleared halfway. The write path itself (encrypt with a
    // key that never leaves the device) is a device-level check.
    private fun storeAddress(address: String?) {
        context.getSharedPreferences("perch.pairing", Context.MODE_PRIVATE).edit().apply {
            if (address == null) remove("address") else putString("address", address)
        }.commit()
    }

    private fun storeTokenBlob(blob: String?) {
        context.getSharedPreferences(prefsName, Context.MODE_PRIVATE).edit().apply {
            if (blob == null) remove("pairing.token") else putString("pairing.token", blob)
        }.commit()
    }

    @Test
    fun anUndecryptableTokenIsNotAPairing() {
        // This is what a restored backup looks like: the preferences arrive, the
        // keystore key that could decrypt them does not, because it never leaves the
        // original device. Sending the blob as a token would earn a 401 and a confusing
        // story; asking to pair again is the truth.
        storeAddress("100.64.0.1:45999")
        storeTokenBlob("not-base64-and-certainly-not-a-ciphertext")
        assertNull(
            "a token the app cannot decrypt must not be used",
            PairingStore(context).load(),
        )
    }

    @Test
    fun halfAPairingIsNotAPairing() {
        val store = PairingStore(context)
        storeAddress(null)
        storeTokenBlob(null)
        assertNull(store.load())

        // An address with no token at all, and a token with no address: both are the
        // state after a partial clear, and neither is something to act on.
        storeAddress("100.64.0.1:45999")
        assertNull("an address without a token is not something to act on", store.load())

        storeAddress(null)
        storeTokenBlob("c29tZS1jaXBoZXJ0ZXh0")
        assertNull("a token without an address is not something to act on", store.load())
    }

    @Test
    fun aCorruptMirrorCacheReadsAsEmpty() {
        // The cache exists so the queue can be read while the computer is unreachable.
        // A corrupt one must read as "nothing cached", because the alternative — a
        // screen claiming to know what is waiting when it does not — is the failure the
        // whole mirror design is arranged to avoid.
        val file = File(context.cacheDir, "mirror-test.json")
        file.writeText("{ this is not json")
        val cache = MirrorCache(file, Json { ignoreUnknownKeys = true })
        assertNull(cache.read())
    }

    @Test
    fun theCacheIsWrittenAtomicallyAndReadBack() {
        val file = File(context.cacheDir, "mirror-roundtrip.json")
        val cache = MirrorCache(file, Json { ignoreUnknownKeys = true })
        val state = dev.perch.console.core.cache.CachedState(
            mirror = dev.perch.console.core.herdrclient.Mirror(),
            decisions = emptyList(),
            savedAtMillis = 1234L,
        )
        cache.write(state)
        val read = cache.read()
        assertEquals(1234L, read?.savedAtMillis)
        // No temporary left behind: the write goes through one, and a stray `.tmp` in
        // the app's cache directory is a second copy of what the user was looking at.
        assertTrue(
            "a temporary file was left behind: ${file.parentFile?.list()?.toList()}",
            file.parentFile?.list()?.none { it.endsWith(".tmp") } ?: true,
        )
    }
}
