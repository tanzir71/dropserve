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
$fixturesRoot = (Resolve-Path -LiteralPath (Join-Path $repositoryRoot "testdata\fixtures")).Path

$start = [System.Diagnostics.ProcessStartInfo]::new()
$start.FileName = $binaryPath
$start.UseShellExecute = $false
$start.CreateNoWindow = $true
$start.RedirectStandardOutput = $true
$start.RedirectStandardError = $true
$start.ArgumentList.Add("serve")
$start.ArgumentList.Add("--listen")
$start.ArgumentList.Add("127.0.0.1:0")
$start.ArgumentList.Add("--root")
$start.ArgumentList.Add($fixturesRoot)

$process = [System.Diagnostics.Process]::Start($start)
try {
    $deadline = [DateTime]::UtcNow.AddSeconds(10)
    $address = ""
    while ([DateTime]::UtcNow -lt $deadline -and $address -eq "") {
        if ($process.HasExited) {
            throw "Dropserve exited before becoming ready: $($process.StandardError.ReadToEnd())"
        }
        $lineTask = $process.StandardOutput.ReadLineAsync()
        if (-not $lineTask.Wait(250)) {
            continue
        }
        if ($lineTask.Result -match '^Dropserve is ready at (http://\S+)$') {
            $address = $Matches[1]
        }
    }
    if ($address -eq "") {
        throw "Dropserve did not print a ready address within 10 seconds"
    }

    $dashboard = Invoke-WebRequest -Uri "$address/" -UseBasicParsing
    if ($dashboard.StatusCode -ne 200 -or $dashboard.Content -notmatch 'id="app-search"') {
        throw "Dashboard returned an unexpected response"
    }
    $apps = Invoke-RestMethod -Uri "$address/_dropserve/api/apps"
    if ($apps.Count -ne 4 -or "static" -notin $apps.slug) {
        throw "Apps API did not return all four fixtures"
    }
    $results = Invoke-RestMethod -Uri "$address/_dropserve/api/search?q=observations"
    if ($results.Count -ne 1 -or $results[0].slug -ne "field-notes") {
        throw "README search did not return field-notes"
    }
    $qr = Invoke-WebRequest -Uri "$address/_dropserve/api/qr?url=$([Uri]::EscapeDataString("$address/field-notes/"))" -UseBasicParsing
    if ($qr.StatusCode -ne 200 -or $qr.Headers["Content-Type"] -ne "image/png" -or $qr.RawContentLength -lt 300) {
        throw "QR endpoint did not return a substantial PNG"
    }
    Write-Output "M2 smoke passed: dashboard rendered 4 apps; README search and local QR succeeded at $address/"
}
finally {
    if (-not $process.HasExited) {
        $process.Kill($true)
        $process.WaitForExit()
    }
    $process.Dispose()
}
