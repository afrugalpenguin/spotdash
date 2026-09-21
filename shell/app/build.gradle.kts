plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// assembleRelease reads its signing key from these and stops if any is unset.
val signingEnv = listOf(
    "SPOTDASH_KEYSTORE_PATH",
    "SPOTDASH_KEYSTORE_PASSWORD",
    "SPOTDASH_KEY_ALIAS",
    "SPOTDASH_KEY_PASSWORD",
)

android {
    namespace = "dev.spotdash.shell"

    // The device runs LineageOS 18.1 (Android 11). compileSdk is newer than
    // targetSdk because the build tools and AndroidX need it.
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.spotdash.shell"
        minSdk = 30
        targetSdk = 30
        // CI passes these from the tag (.github/scripts/apk-version.sh).
        versionCode = (findProperty("versionCode") as String?)?.toInt() ?: 1
        versionName = (findProperty("versionName") as String?) ?: "0.1.0"
    }

    signingConfigs {
        create("release") {
            storeFile = System.getenv("SPOTDASH_KEYSTORE_PATH")?.let { File(it) }
            storePassword = System.getenv("SPOTDASH_KEYSTORE_PASSWORD")
            keyAlias = System.getenv("SPOTDASH_KEY_ALIAS")
            keyPassword = System.getenv("SPOTDASH_KEY_PASSWORD")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.getByName("release")
        }
    }

    lint {
        // The shell is sideloaded and targets Android 11, so the Play Store floor does not apply.
        disable += "ExpiredTargetSdkVersion"
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        viewBinding = false
    }
}

val checkReleaseSigning by tasks.registering {
    doLast {
        val missing = signingEnv.filter { System.getenv(it).isNullOrEmpty() }
        if (missing.isNotEmpty()) {
            throw GradleException("Release signing input not set: ${missing.joinToString(", ")}")
        }
    }
}

// Runs first in any release build, so a missing input fails before compiling.
tasks.matching { it.name == "preReleaseBuild" }.configureEach {
    dependsOn(checkReleaseSigning)
}

dependencies {
    implementation("androidx.appcompat:appcompat:1.7.0")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")

    testImplementation("junit:junit:4.13.2")
}
