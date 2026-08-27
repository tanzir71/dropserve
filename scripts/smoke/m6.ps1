param(
    [string]$Binary = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$PSNativeCommandUseErrorActionPreference = $false

function Invoke-Autostart {
    param([string]$Action)

    $output = @(& $script:binaryPath autostart $Action 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "dropserve autostart $Action failed: $($output -join [Environment]::NewLine)"
    }
    return $output -join [Environment]::NewLine
}

$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if ($Binary -eq "") {
    $Binary = Join-Path $repositoryRoot "bin\dropserve.exe"
}
$script:binaryPath = (Resolve-Path -LiteralPath $Binary).Path
$backupPath = Join-Path ([System.IO.Path]::GetTempPath()) ("dropserve-task-backup-" + [Guid]::NewGuid().ToString("N") + ".xml")
$existingTask = @(& schtasks.exe /Query /TN Dropserve /XML 2>$null)
$hadExistingTask = $LASTEXITCODE -eq 0
if ($hadExistingTask) {
    [System.IO.File]::WriteAllText(
        $backupPath,
        $existingTask -join [Environment]::NewLine,
        [System.Text.Encoding]::Unicode
    )
}

try {
    $null = Invoke-Autostart -Action "enable"
    $null = Invoke-Autostart -Action "enable"
    $taskLines = @(& schtasks.exe /Query /TN Dropserve /XML 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "schtasks could not query the enabled Dropserve task: $($taskLines -join [Environment]::NewLine)"
    }
    $taskXML = $taskLines -join [Environment]::NewLine
    foreach ($required in @(
        "<LogonTrigger>",
        "<LogonType>InteractiveToken</LogonType>",
        "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
        "<DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>",
        "<StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>",
        "<Delay>PT10S</Delay>",
        "<Count>3</Count>",
        "<Interval>PT1M</Interval>",
        "<Arguments>--background</Arguments>"
    )) {
        if (-not $taskXML.Contains($required)) {
            throw "Scheduled Task XML does not contain $required"
        }
    }
    if ($taskXML.Contains("HighestAvailable")) {
        throw "Scheduled Task XML requests elevated privileges"
    }
    if (-not $taskXML.Contains("<Command>$script:binaryPath</Command>")) {
        throw "Scheduled Task action does not use $script:binaryPath"
    }
    $status = Invoke-Autostart -Action "status"
    if ($status.Trim() -ne "enabled") {
        throw "autostart status reported $status instead of enabled"
    }

    $deleteOutput = @(& schtasks.exe /Delete /TN Dropserve /F 2>&1)
    if ($LASTEXITCODE -ne 0) {
        throw "external schtasks deletion failed: $($deleteOutput -join [Environment]::NewLine)"
    }
    $status = Invoke-Autostart -Action "status"
    if ($status.Trim() -ne "disabled") {
        throw "autostart status reported $status after external deletion instead of disabled"
    }

    $null = Invoke-Autostart -Action "enable"
    $null = Invoke-Autostart -Action "disable"
    $null = Invoke-Autostart -Action "disable"

    "M6 Windows autostart smoke passed: safe registration, OS-backed status, and idempotence were verified."
}
finally {
    $cleanupOutput = @(& $script:binaryPath autostart disable 2>&1)
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "Could not remove the smoke task: $($cleanupOutput -join [Environment]::NewLine)"
    }
    if ($hadExistingTask) {
        $restoreOutput = @(& schtasks.exe /Create /XML $backupPath /TN Dropserve /F 2>&1)
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Could not restore the pre-existing Dropserve task: $($restoreOutput -join [Environment]::NewLine)"
        }
    }
    if (Test-Path -LiteralPath $backupPath) {
        Remove-Item -LiteralPath $backupPath -Force
    }
}
