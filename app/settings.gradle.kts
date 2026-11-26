// Perch — Android client.
//
// Single Gradle module on purpose: the app is one user-facing product, and a
// premature :core / :feature split would be architecture for its own sake. The
// package layout inside the module carries the structure instead.
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "Perch"
include(":app")
