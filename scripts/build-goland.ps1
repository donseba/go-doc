$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$jdkHome = Join-Path $env:USERPROFILE "scoop\apps\temurin17-jdk\current"

if (Test-Path $jdkHome) {
    $env:JAVA_HOME = $jdkHome
    $env:Path = "$env:JAVA_HOME\bin;$env:USERPROFILE\scoop\shims;$env:Path"
}

Push-Location (Join-Path $root "ide\goland")
try {
    gradle buildPlugin
} finally {
    Pop-Location
}
