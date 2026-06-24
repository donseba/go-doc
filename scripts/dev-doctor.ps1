$ErrorActionPreference = "Stop"

foreach ($tool in @("go", "node", "npx", "java", "gradle")) {
    $cmd = Get-Command $tool -ErrorAction SilentlyContinue
    if ($cmd) {
        Write-Host "[ok] $tool -> $($cmd.Source)"
    } elseif ($tool -eq "java" -and (Test-Path (Join-Path $env:USERPROFILE "scoop\apps\temurin17-jdk\current\bin\java.exe"))) {
        Write-Host "[ok] java -> $(Join-Path $env:USERPROFILE "scoop\apps\temurin17-jdk\current\bin\java.exe")"
    } else {
        Write-Host "[missing] $tool"
    }
}
