param(
    [string]$Binary = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Get-ResponseText {
    param([object]$Response)
    if ($Response.Content -is [byte[]]) {
        return [System.Text.Encoding]::UTF8.GetString($Response.Content)
    }
    return [string]$Response.Content
}

$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if ($Binary -eq "") {
    $Binary = Join-Path $repositoryRoot "bin\dropserve-cli.exe"
}
$binaryPath = (Resolve-Path -LiteralPath $Binary).Path
$fixturesRoot = (Resolve-Path -LiteralPath (Join-Path $repositoryRoot "testdata\fixtures")).Path
$workDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ("dropserve-m4-smoke-" + [Guid]::NewGuid().ToString("N"))
$null = New-Item -ItemType Directory -Path $workDirectory
$statePath = Join-Path $workDirectory "state.json"

$start = [System.Diagnostics.ProcessStartInfo]::new()
$start.FileName = $binaryPath
$start.UseShellExecute = $false
$start.CreateNoWindow = $true
$start.RedirectStandardOutput = $true
$start.RedirectStandardError = $true
foreach ($argument in @("serve", "--listen", "127.0.0.1:0", "--root", $fixturesRoot, "--state", $statePath)) {
    $start.ArgumentList.Add($argument)
}

$process = [System.Diagnostics.Process]::Start($start)
try {
    $readyLine = $process.StandardOutput.ReadLineAsync()
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    while (-not $readyLine.IsCompleted -and [DateTime]::UtcNow -lt $deadline) {
        if ($process.HasExited) {
            throw "Dropserve exited before becoming ready: $($process.StandardError.ReadToEnd())"
        }
        $null = $readyLine.Wait(250)
    }
    if (-not $readyLine.IsCompleted -or $readyLine.Result -notmatch '^Dropserve is ready at (http://\S+)$') {
        throw "Dropserve did not print a ready address within 45 seconds"
    }
    $address = $Matches[1]

    $node = Invoke-WebRequest -Uri "$address/node/" -UseBasicParsing
    if ($node.StatusCode -ne 200 -or (Get-ResponseText $node) -ne "Dropserve Node fixture") {
        throw "Node fixture returned an unexpected response"
    }
    $python = Invoke-WebRequest -Uri "$address/python/" -UseBasicParsing
    if ($python.StatusCode -ne 200 -or (Get-ResponseText $python) -ne "Dropserve Python fixture") {
        throw "Python fixture returned an unexpected response"
    }
    $dashboard = Invoke-WebRequest -Uri "$address/" -UseBasicParsing
    if ($dashboard.StatusCode -ne 200 -or $dashboard.Content -notmatch 'id="log-dialog"') {
        throw "Dashboard did not render the command log viewer"
    }
    $apps = Invoke-RestMethod -Uri "$address/_dropserve/api/apps"
    $broken = @($apps | Where-Object { $_.slug -eq "broken" })
    if ($broken.Count -ne 1 -or $broken[0].status -ne "crashed") {
        throw "Broken fixture was not isolated in crashed state"
    }
    $logs = Invoke-RestMethod -Uri "$address/_dropserve/api/logs/broken"
    if ($logs.status -ne "crashed" -or $logs.attempts -ne 5 -or $logs.logs -notmatch "intentional fixture failure") {
        throw "Broken fixture logs did not preserve all five failed starts"
    }
    Write-Output "M4 smoke passed: Node and Python returned 200; broken was isolated after 5 starts with logs at $address/"
}
finally {
    if (-not $process.HasExited) {
        $process.Kill($true)
        $process.WaitForExit()
    }
    $process.Dispose()
    if (Test-Path -LiteralPath $workDirectory) {
        Remove-Item -LiteralPath $workDirectory -Recurse -Force
    }
}
