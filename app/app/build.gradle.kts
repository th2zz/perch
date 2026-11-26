import java.util.Properties

plugins {
    alias(libs.plugins.android.application)
    // No `org.jetbrains.kotlin.android` here: AGP 9 compiles Kotlin itself, and
    // applying that plugin now fails the build outright. Only the compiler
    // plugins that add language features are applied.
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

/**
 * Release signing, from wherever the key happens to be on this machine.
 *
 * An unsigned release APK cannot be installed by anyone, so "build a release" and
 * "have something to hand a user" are the same task, and this is where they meet.
 *
 * The key is **not** in the repository and must not be: it is the app's identity,
 * whoever holds it can ship updates Android accepts as Perch, and it cannot be
 * rotated without every installed copy being uninstalled first. The values come
 * from (in order) environment variables, then `keystore.properties` next to the
 * app, then, if neither is there, no signing config at all — which keeps a fresh
 * clone building and every existing test running without the key.
 */
val keystoreProperties = Properties().also { props ->
    val file = rootProject.file("keystore.properties")
    if (file.exists()) file.inputStream().use { stream -> props.load(stream) }
}

fun secret(name: String, property: String): String? =
    System.getenv(name)?.takeIf { it.isNotBlank() } ?: keystoreProperties.getProperty(property)?.takeIf { it.isNotBlank() }

val releaseStoreFile = secret("PERCH_KEYSTORE", "storeFile")
val signingConfigured = releaseStoreFile != null &&
    secret("PERCH_KEYSTORE_PASSWORD", "storePassword") != null &&
    secret("PERCH_KEY_ALIAS", "keyAlias") != null &&
    secret("PERCH_KEY_PASSWORD", "keyPassword") != null

android {
    namespace = "dev.perch.console"
    // Compile against the newest platform because AndroidX Compose now requires
    // it; target a settled release because targetSdk opts the app into new
    // runtime behaviour, and each of those behaviours needs to be verified on a
    // real device before it becomes the product's default.
    compileSdk = 37

    defaultConfig {
        applicationId = "dev.perch.console"
        // Android 13 and up: the spec's floor. Notifications, the IME behaviour
        // the product depends on, and per-app language handling are all settled
        // by then, so supporting older releases would buy users the product
        // cannot serve.
        minSdk = 33
        targetSdk = 36
        versionCode = 21
        versionName = "0.6.1"
        buildConfigField(
            "boolean",
            "SESSION_SCREEN_ONLY",
            providers.gradleProperty("perch.sessionScreenOnly").orElse("true").get().toBooleanStrict().toString(),
        )
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }

    // Declared before the build types that reference it: Gradle evaluates this
    // block in order, and a signing config mentioned before it exists is an error.
    signingConfigs {
        if (signingConfigured) {
            create("release") {
                storeFile = file(releaseStoreFile!!)
                storePassword = secret("PERCH_KEYSTORE_PASSWORD", "storePassword")
                keyAlias = secret("PERCH_KEY_ALIAS", "keyAlias")
                keyPassword = secret("PERCH_KEY_PASSWORD", "keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            // Shipping the unused resources of every library in the APK would be
            // someone else's code taking up the user's space.
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro",
            )
            if (signingConfigured) {
                signingConfig = signingConfigs.getByName("release")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        buildConfig = true
        compose = true
    }

    testOptions {
        unitTests {
            // Compose renders its own views, which need the app's resources on the
            // test classpath. Without this the rendering tests below see nothing.
            isIncludeAndroidResources = true
        }
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }

    lint {
        // Real errors fail the build, which is what makes lint worth running: it
        // is the only static check that understands Android's own APIs, and the
        // mistakes it catches are the ones that would otherwise appear as a crash
        // on somebody's phone.
        abortOnError = true
        // Version-currency advice is not a defect, and every pin here is a
        // deliberate decision with its reason written next to it: Kotlin matches
        // the version AGP ships, targetSdk stays a release behind compileSdk until
        // the newer behaviours have been checked on a device. Leaving them on
        // would train whoever runs lint to ignore its output.
        disable += setOf("GradleDependency", "NewerVersionAvailable", "OldTargetApi")
        // Lint's own dependency scan duplicates the advice above, at more length.
        checkDependencies = false
    }

}

/**
 * Builds the host-side bridge so the app's own client code can be exercised
 * against it.
 *
 * Everything the product does crosses the boundary between these two programs,
 * and the two have been written in different languages by design. Unit tests on
 * each side of that boundary can both pass while the pair is broken — a field
 * renamed on one side, a status code changed on the other — so the tests that
 * matter most are the ones where the real client talks to the real server.
 *
 * The task is best-effort: on a machine without Go the binary is simply absent
 * and the integration tests skip, rather than failing a build for a tool the
 * developer may not need.
 */
val bridgeProjectDir = rootProject.projectDir.parentFile.resolve("bridge")
val bridgeBinary = layout.buildDirectory.file("perch-bridge/perch-bridge")

val buildBridgeForTests = tasks.register<Exec>("buildBridgeForTests") {
    group = "verification"
    description = "Builds the bridge binary used by the app's integration tests."
    workingDir = bridgeProjectDir
    commandLine("go", "build", "-o", bridgeBinary.get().asFile.absolutePath, "./cmd/perch-bridge")
    isIgnoreExitValue = true
    inputs.files(
        fileTree(bridgeProjectDir) {
            include("**/*.go")
            include("go.mod")
        },
    ).withPathSensitivity(PathSensitivity.RELATIVE)
    outputs.file(bridgeBinary)
    // No `upToDateWhen { binary exists }` here. That looked like a cheap way to
    // skip a rebuild, and it silently made Gradle skip the task *always*, so the
    // integration tests ran against a stale binary — a green suite describing
    // code that no longer existed. The declared inputs above are the right way to
    // decide this: it rebuilds when the bridge's sources change and not otherwise.
}

tasks.withType<Test>().configureEach {
    dependsOn(buildBridgeForTests)
    // Passed on the command line rather than through an environment variable so
    // the test can tell "the bridge was not built" from "the bridge failed".
    systemProperty("perch.bridge.bin", bridgeBinary.get().asFile.absolutePath)

    // The contract files the bridge writes and the app reads — the bytes of a
    // pairing link, the names of the event types — are inputs to these tests, and
    // they have to be declared: without this, editing one does not make Gradle
    // re-run them, so a broken seam stays green until someone happens to touch a
    // Kotlin file. That is the same trap the bridge-build task above carries a
    // comment about — a stale green describing a pair that no longer agrees.
    //
    // The whole JSON set rather than a named file, so a contract added later is
    // covered without anyone remembering to come back here; the markdown beside
    // them is prose and deliberately left out, so editing a document does not
    // re-run the suite.
    inputs.files(
        fileTree(rootProject.projectDir.parentFile.resolve("contracts")) {
            include("**/*.json")
        },
    )
        .withPropertyName("sharedContracts")
        .withPathSensitivity(PathSensitivity.RELATIVE)
}

dependencies {
    implementation(libs.androidx.core.ktx)
    implementation(libs.androidx.lifecycle.runtime.ktx)
    implementation(libs.androidx.lifecycle.viewmodel.compose)
    implementation(libs.androidx.lifecycle.viewmodel.ktx)
    implementation(libs.androidx.lifecycle.runtime.compose)
    implementation(libs.androidx.activity.compose)

    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.ui.graphics)
    implementation(libs.androidx.compose.ui.tooling.preview)
    implementation(libs.androidx.compose.material3)
    debugImplementation(libs.androidx.compose.ui.tooling)

    implementation(libs.okhttp)
    implementation(libs.kotlinx.serialization.json)
    implementation(libs.kotlinx.coroutines.android)

    testImplementation(libs.junit)
    testImplementation(libs.kotlinx.coroutines.test)
    // Rendering the screens on the JVM: the closest thing to looking at the app
    // that does not need a device.
    testImplementation(libs.robolectric)
    testImplementation(libs.androidx.test.core)
    testImplementation(platform(libs.androidx.compose.bom))
    testImplementation(libs.androidx.compose.ui.test.junit4)
    debugImplementation(libs.androidx.compose.ui.test.manifest)
}
