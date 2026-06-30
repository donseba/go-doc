plugins {
    id("org.jetbrains.kotlin.jvm") version "2.2.0"
    id("org.jetbrains.intellij.platform")
}

group = "com.donseba.godoc"
version = "0.13.4"

kotlin {
    jvmToolchain(21)
}

intellijPlatform {
    pluginConfiguration {
        name = "go-doc"
        version = project.version.toString()
        description = """
            <p>
              <b>go-doc</b> adds typed Go template support to GoLand for
              <code>.gohtml</code>, <code>.tmpl</code>, and <code>.html</code> files.
            </p>
            <p>It runs the shared go-doc language server and provides:</p>
            <ul>
              <li>contract-aware completions and diagnostics</li>
              <li>hover, go-to-definition, and document symbols</li>
              <li>semantic highlighting for roots, fields, methods, functions, generated namespaces, and included templates</li>
              <li>static FuncMap discovery and provider package support</li>
            </ul>
            <p>
              Declare template contracts with annotations such as <code>@model</code>,
              <code>@dot</code>, <code>@func</code>, and <code>@gen</code>, or use
              <code>.go-doc/config.json</code> for shared project configuration.
            </p>
            <p><b>Static analysis only:</b> go-doc never executes application code.</p>
        """.trimIndent()
        ideaVersion {
            sinceBuild = "253"
        }
    }
}

dependencies {
    intellijPlatform {
        goland("2025.3")
        bundledPlugin("org.jetbrains.plugins.go-template")
    }
}


