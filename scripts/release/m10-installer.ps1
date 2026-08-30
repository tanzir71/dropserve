param(
    [Parameter(Mandatory = $true)]
    [string]$Installer,
    [switch]$CurrentUser
)

$ErrorActionPreference = 'Stop'
$installerPath = (Resolve-Path -LiteralPath $Installer).Path
$sandbox = Join-Path ([System.IO.Path]::GetTempPath()) ("dropserve-installer-smoke-" + [Guid]::NewGuid().ToString('N'))
$installDirectory = Join-Path $sandbox 'installed'
$appsDirectory = Join-Path $sandbox 'Apps'
$statePath = Join-Path $sandbox 'state\state.json'
$server = $null
$ownedLocalDataDirectory = Join-Path $env:LOCALAPPDATA 'Dropserve'
$ownedRoamingDataDirectory = Join-Path $env:APPDATA 'Dropserve'
$defaultAppsContainer = Join-Path $env:USERPROFILE 'Dropserve'
$defaultAppsDirectory = Join-Path $defaultAppsContainer 'Apps'
$preservedAppMarker = Join-Path $defaultAppsDirectory 'uninstall-preservation-proof.txt'
$ownedDataPrepared = $false
$defaultAppsPrepared = $false

foreach ($path in @($ownedLocalDataDirectory, $ownedRoamingDataDirectory, $defaultAppsContainer)) {
    if (Test-Path -LiteralPath $path) {
        throw "installer smoke requires a clean runner; refusing to touch pre-existing path $path"
    }
}

New-Item -ItemType Directory -Force -Path $appsDirectory | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $appsDirectory 'hello') | Out-Null
Set-Content -LiteralPath (Join-Path $appsDirectory 'hello\index.html') -Value '<h1>installed Dropserve</h1>' -Encoding utf8NoBOM

try {
    $installArguments = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=$installDirectory")
    if ($CurrentUser) {
        $installArguments += '/CURRENTUSER'
    }
    $install = Start-Process -FilePath $installerPath -ArgumentList $installArguments -Wait -PassThru -WindowStyle Hidden
    if ($install.ExitCode -ne 0) {
        throw "silent installer exited $($install.ExitCode)"
    }

    $desktopBinary = Join-Path $installDirectory 'dropserve.exe'
    $cliBinary = Join-Path $installDirectory 'dropserve-cli.exe'
    if (!(Test-Path -LiteralPath $desktopBinary -PathType Leaf) -or !(Test-Path -LiteralPath $cliBinary -PathType Leaf)) {
        throw 'installer did not place both Dropserve binaries'
    }

    if (!$CurrentUser) {
        $firewall = & netsh.exe advfirewall firewall show rule name=Dropserve verbose 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0 -or $firewall -notmatch [regex]::Escape($desktopBinary)) {
            throw "installer did not create the exact Dropserve firewall rule: $firewall"
        }
    }

    $stdoutPath = Join-Path $sandbox 'server.stdout.log'
    $stderrPath = Join-Path $sandbox 'server.stderr.log'
    $server = Start-Process -FilePath $cliBinary -ArgumentList @('serve', '--root', $appsDirectory, '--state', $statePath, '--bind', '127.0.0.1') -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    $healthy = $false
    $healthOutput = ''
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    while ([DateTime]::UtcNow -lt $deadline) {
        $healthOutput = (& $cliBinary healthz --state $statePath 2>&1 | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $healthOutput -eq 'ok') {
            $healthy = $true
            break
        }
        if ($server.HasExited) {
            break
        }
        Start-Sleep -Milliseconds 200
    }
    if (!$healthy) {
        $stderrText = if (Test-Path -LiteralPath $stderrPath) { Get-Content -LiteralPath $stderrPath -Raw } else { '' }
        throw "installed dropserve healthz failed: $healthOutput; stderr=$stderrText"
    }

    $autostart = Start-Process -FilePath $cliBinary -ArgumentList @('autostart', 'enable') -Wait -PassThru -WindowStyle Hidden
    if ($autostart.ExitCode -ne 0) {
        throw "installed Dropserve could not create its autostart task: $($autostart.ExitCode)"
    }

    New-Item -ItemType Directory -Force -Path $ownedLocalDataDirectory,$ownedRoamingDataDirectory | Out-Null
    Set-Content -LiteralPath (Join-Path $ownedLocalDataDirectory 'uninstall-proof.txt') -Value 'Dropserve-owned local data' -Encoding utf8NoBOM
    Set-Content -LiteralPath (Join-Path $ownedRoamingDataDirectory 'uninstall-proof.txt') -Value 'Dropserve-owned configuration data' -Encoding utf8NoBOM
    $ownedDataPrepared = $true

    New-Item -ItemType Directory -Force -Path $defaultAppsDirectory | Out-Null
    Set-Content -LiteralPath $preservedAppMarker -Value 'user-owned app data' -Encoding utf8NoBOM
    $defaultAppsPrepared = $true

    $uninstaller = Join-Path $installDirectory 'unins000.exe'
    if (!(Test-Path -LiteralPath $uninstaller -PathType Leaf)) {
        throw 'installer did not create its uninstaller'
    }
    $uninstall = Start-Process -FilePath $uninstaller -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -PassThru -WindowStyle Hidden
    if ($uninstall.ExitCode -ne 0) {
        throw "silent uninstaller exited $($uninstall.ExitCode)"
    }
    if (!$server.WaitForExit(10000)) {
        throw 'uninstaller did not stop the running Dropserve process'
    }
    $server = $null

    $deleteDeadline = [DateTime]::UtcNow.AddSeconds(10)
    while ((Test-Path -LiteralPath $installDirectory) -and [DateTime]::UtcNow -lt $deleteDeadline) {
        Start-Sleep -Milliseconds 100
    }
    if (Test-Path -LiteralPath $installDirectory) {
        $leftovers = Get-ChildItem -LiteralPath $installDirectory -Force -Recurse | ForEach-Object { $_.FullName.Substring($installDirectory.Length).TrimStart('\') }
        throw "uninstaller left the install directory: $installDirectory; contents=$($leftovers -join ', ')"
    }
    & schtasks.exe /Query /TN Dropserve *> $null
    if ($LASTEXITCODE -eq 0) {
        throw 'uninstaller left the Dropserve Scheduled Task'
    }
    $firewallAfter = & netsh.exe advfirewall firewall show rule name=Dropserve 2>&1 | Out-String
    if ($firewallAfter -match 'Rule Name:\s+Dropserve') {
        throw "uninstaller left the Dropserve firewall rule: $firewallAfter"
    }
    foreach ($path in @($ownedLocalDataDirectory, $ownedRoamingDataDirectory)) {
        if (Test-Path -LiteralPath $path) {
            throw "uninstaller left Dropserve-owned data: $path"
        }
    }
    if (!(Test-Path -LiteralPath $preservedAppMarker -PathType Leaf)) {
        throw "uninstaller removed the user's Apps folder or its contents: $preservedAppMarker"
    }

    Write-Output 'M10 installer smoke passed: silent install, healthz, complete owned-data cleanup, user Apps preservation, and silent uninstall.'
}
finally {
    if ($null -ne $server -and !$server.HasExited) {
        Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue
        $server.WaitForExit(5000) | Out-Null
    }
    if (Test-Path -LiteralPath $installDirectory) {
        $uninstaller = Join-Path $installDirectory 'unins000.exe'
        if (Test-Path -LiteralPath $uninstaller -PathType Leaf) {
            Start-Process -FilePath $uninstaller -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -WindowStyle Hidden | Out-Null
        }
    }
    if ($ownedDataPrepared) {
        foreach ($path in @($ownedLocalDataDirectory, $ownedRoamingDataDirectory)) {
            if (Test-Path -LiteralPath $path) {
                Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }
    if ($defaultAppsPrepared -and (Test-Path -LiteralPath $preservedAppMarker -PathType Leaf)) {
        Remove-Item -LiteralPath $preservedAppMarker -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $defaultAppsDirectory -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $defaultAppsContainer -Force -ErrorAction SilentlyContinue
    }
    $temporaryRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    $resolvedSandbox = [System.IO.Path]::GetFullPath($sandbox)
    if ($resolvedSandbox.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -and $resolvedSandbox -ne $temporaryRoot) {
        Remove-Item -LiteralPath $resolvedSandbox -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Expected negative native probes above can leave $LASTEXITCODE non-zero even
# after every assertion passes. Return success explicitly; terminating errors
# thrown in the try block still propagate after finally and never reach here.
exit 0
