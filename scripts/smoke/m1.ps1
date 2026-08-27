param(
    [string]$Binary = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if ($Binary -eq "") {
    $Binary = Join-Path $repositoryRoot "bin\dropserve-cli.exe"
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
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    $lineTask = $process.StandardOutput.ReadLineAsync()
    while (-not $lineTask.IsCompleted -and [DateTime]::UtcNow -lt $deadline) {
        if ($process.HasExited) {
            $failure = $process.StandardError.ReadToEnd()
            throw "Dropserve exited before becoming ready: $failure"
        }
        $null = $lineTask.Wait(250)
    }
    if (-not $lineTask.IsCompleted -or $lineTask.Result -notmatch '^Dropserve is ready at (http://\S+)$') {
        throw "Dropserve did not print a ready address within 45 seconds"
    }
    $address = $Matches[1]

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
