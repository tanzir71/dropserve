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
            $failure = $process.StandardError.ReadToEnd()
            throw "Dropserve exited before becoming ready: $failure"
        }
        $lineTask = $process.StandardOutput.ReadLineAsync()
        if (-not $lineTask.Wait(250)) {
            continue
        }
        $line = $lineTask.Result
        if ($line -match '^Dropserve is ready at (http://\S+)$') {
            $address = $Matches[1]
        }
    }
    if ($address -eq "") {
        throw "Dropserve did not print a ready address within 10 seconds"
    }

    $response = Invoke-WebRequest -Uri "$address/static/" -UseBasicParsing
    if ($response.StatusCode -ne 200 -or $response.Content -notmatch '<h1>Static fixture</h1>') {
        throw "Mounted fixture returned an unexpected response"
    }
    Write-Output "M1 smoke passed: $address/static/ returned 200"
}
finally {
    if (-not $process.HasExited) {
        $process.Kill($true)
        $process.WaitForExit()
    }
    $process.Dispose()
}

