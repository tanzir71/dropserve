param(
    [string]$Binary = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if ($Binary -eq "") {
    $Binary = Join-Path $repositoryRoot "bin\dropserve.exe"
}
$binaryPath = (Resolve-Path -LiteralPath $Binary).Path

$image = [System.IO.File]::ReadAllBytes($binaryPath)
if ($image.Length -lt 256 -or $image[0] -ne 0x4d -or $image[1] -ne 0x5a) {
    throw "$binaryPath is not a valid Windows PE executable"
}
$peOffset = [System.BitConverter]::ToInt32($image, 0x3c)
$subsystemOffset = $peOffset + 24 + 68
if ($subsystemOffset + 1 -ge $image.Length) {
    throw "$binaryPath has a truncated PE optional header"
}
$subsystem = [System.BitConverter]::ToUInt16($image, $subsystemOffset)
if ($subsystem -ne 2) {
    throw "$binaryPath uses PE subsystem $subsystem instead of IMAGE_SUBSYSTEM_WINDOWS_GUI (2)"
}

$temporaryRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$workDirectory = Join-Path $temporaryRoot ("dropserve-background-smoke-" + [Guid]::NewGuid().ToString("N"))
$appsRoot = Join-Path $workDirectory "apps"
$probePath = Join-Path $workDirectory "console-window.txt"
$null = New-Item -ItemType Directory -Path $appsRoot
$process = $null

try {
    $start = [System.Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $binaryPath
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $false
    $start.Environment["DROPSERVE_BACKGROUND_CONSOLE_PROBE"] = $probePath
    foreach ($argument in @("--background", "--listen", "127.0.0.1:0", "--root", $appsRoot)) {
        $start.ArgumentList.Add($argument)
    }

    $process = [System.Diagnostics.Process]::Start($start)
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    while (-not (Test-Path -LiteralPath $probePath) -and [DateTime]::UtcNow -lt $deadline) {
        if ($process.HasExited) {
            throw "The GUI background process exited before reporting its console-window handle"
        }
        Start-Sleep -Milliseconds 100
    }
    if (-not (Test-Path -LiteralPath $probePath)) {
        throw "The GUI background process did not report its console-window handle within 15 seconds"
    }
    $handle = ([System.IO.File]::ReadAllText($probePath)).Trim()
    if ($handle -ne "0") {
        throw "GetConsoleWindow() returned $handle for the GUI background process; expected 0"
    }

    "M6 Windows background smoke passed: the PE uses the GUI subsystem and GetConsoleWindow() returned 0."
}
finally {
    if ($null -ne $process) {
        if (-not $process.HasExited) {
            $process.Kill($true)
            $process.WaitForExit()
        }
        $process.Dispose()
    }
    $resolvedWork = [System.IO.Path]::GetFullPath($workDirectory)
    if ($resolvedWork.StartsWith($temporaryRoot, [System.StringComparison]::OrdinalIgnoreCase) -and
        $resolvedWork -ne $temporaryRoot -and
        (Test-Path -LiteralPath $resolvedWork)) {
        Remove-Item -LiteralPath $resolvedWork -Recurse -Force
    }
}
