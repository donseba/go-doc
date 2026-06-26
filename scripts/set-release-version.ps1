param(
    [string]$Tag = $env:CIRCLE_TAG
)

$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($Tag)) {
    Write-Error "release tag is required, for example v0.2.5"
}

$Version = $Tag.TrimStart("v")
if ($Version -notmatch "^[0-9]+\.[0-9]+\.[0-9]+$") {
    Write-Error "release tag must look like vMAJOR.MINOR.PATCH; got $Tag"
}

$Root = Split-Path -Parent $PSScriptRoot

function Write-Utf8NoBom {
    param(
        [string]$Path,
        [string]$Text
    )
    $Encoding = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Text, $Encoding)
}

$GradlePath = Join-Path $Root "ide\goland\build.gradle.kts"
$Gradle = Get-Content $GradlePath -Raw
$Gradle = $Gradle -replace '(?m)^version\s*=\s*"[^"]+"', "version = `"$Version`""
Write-Utf8NoBom $GradlePath $Gradle

function Set-FirstJsonVersion {
    param(
        [string]$Path,
        [string]$Version
    )
    $Text = Get-Content $Path -Raw
    $Regex = [regex]'("version"\s*:\s*")[^"]+(")'
    $Text = $Regex.Replace($Text, "`${1}$Version`${2}", 1)
    Write-Utf8NoBom $Path $Text
}

function Set-FirstTwoJsonVersions {
    param(
        [string]$Path,
        [string]$Version
    )
    $Text = Get-Content $Path -Raw
    $Regex = [regex]'("version"\s*:\s*")[^"]+(")'
    $Text = $Regex.Replace($Text, "`${1}$Version`${2}", 2)
    Write-Utf8NoBom $Path $Text
}

Set-FirstJsonVersion (Join-Path $Root "ide\vscode\package.json") $Version
Set-FirstTwoJsonVersions (Join-Path $Root "ide\vscode\package-lock.json") $Version
Set-FirstJsonVersion (Join-Path $Root "ide\sublime\sublime-package.json") $Version

$GoVersionPath = Join-Path $Root "internal\godoccli\version.go"
$GoVersion = Get-Content $GoVersionPath -Raw
$GoVersion = $GoVersion -replace 'const Version = "[^"]+"', "const Version = `"$Version`""
Write-Utf8NoBom $GoVersionPath $GoVersion

Write-Host "release manifests set to $Version"
