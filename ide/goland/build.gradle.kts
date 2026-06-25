plugins {
    id("org.jetbrains.kotlin.jvm") version "2.2.0"
    id("org.jetbrains.intellij.platform")
}

group = "com.donseba.godoc"
version = "0.8.27"

kotlin {
    jvmToolchain(17)
}

intellijPlatform {
    pluginConfiguration {
        name = "go-doc"
        version = project.version.toString()
        description = "Template contract completion and diagnostics for Go template files."
        ideaVersion {
            sinceBuild = "241"
        }
    }
}

dependencies {
    intellijPlatform {
        goland("2025.3")
        bundledPlugin("org.jetbrains.plugins.go-template")
    }
}


