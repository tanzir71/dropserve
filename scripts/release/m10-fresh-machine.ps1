param(
    [Parameter(Mandatory = $true)]
    [string]$Installer,
    [switch]$CurrentUser
)

$ErrorActionPreference = 'Stop'
$alpineURL = 'https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/x86_64/alpine-minirootfs-3.24.1-x86_64.tar.gz'
$alpineSHA256 = '41f73e3cf5fa919b8aa5ca6b30dc48f0da2720776d7423e2a7748211456fe081'
$installerPath = (Resolve-Path -LiteralPath $Installer).Path
$runID = [Guid]::NewGuid().ToString('N')
$distro = 'DropserveM10Fresh-' + $runID.Substring(0, 12)
$sandbox = Join-Path ([System.IO.Path]::GetTempPath()) ('dropserve-fresh-machine-' + $runID)
$installDirectory = Join-Path $sandbox 'installed'
$appsDirectory = Join-Path $sandbox 'Apps'
$appDirectory = Join-Path $appsDirectory 'fresh-machine'
$statePath = Join-Path $sandbox 'state\state.json'
$archivePath = Join-Path $sandbox 'alpine-minirootfs.tar.gz'
$wslDirectory = Join-Path $sandbox 'wsl'
$server = $null
$distroRegistered = $false
$transcript = [System.Collections.Generic.List[string]]::new()

New-Item -ItemType Directory -Force -Path $appDirectory,$wslDirectory | Out-Null
Set-Content -LiteralPath (Join-Path $appDirectory 'index.html') -Value '<h1>fresh-machine network proof</h1>' -Encoding utf8NoBOM

try {
    Invoke-WebRequest -Uri $alpineURL -OutFile $archivePath -TimeoutSec 60
    $actualHash = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $alpineSHA256) {
        throw "Alpine guest checksum mismatch: got $actualHash, want $alpineSHA256"
    }

    & wsl.exe --import $distro $wslDirectory $archivePath --version 2
    if ($LASTEXITCODE -ne 0) {
        throw "WSL2 guest import exited $LASTEXITCODE"
    }
    $distroRegistered = $true
    $guestKernel = (& wsl.exe -d $distro -- uname -srm | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $guestKernel -notmatch '^Linux ') {
        throw "disposable guest did not report a Linux kernel: $guestKernel"
    }
    $guestRoutes = (& wsl.exe -d $distro -- ip route | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or $guestRoutes -notmatch 'default via ([0-9.]+)') {
        throw "disposable guest has no virtual-network route: $guestRoutes"
    }
    $windowsGuestAddress = $Matches[1]

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
        throw 'fresh Windows install did not contain both Dropserve binaries'
    }

    $stdoutPath = Join-Path $sandbox 'server.stdout.log'
    $stderrPath = Join-Path $sandbox 'server.stderr.log'
    $server = Start-Process -FilePath $desktopBinary -ArgumentList @('serve', '--root', $appsDirectory, '--state', $statePath, '--bind', '0.0.0.0') -PassThru -WindowStyle Hidden -RedirectStandardOutput $stdoutPath -RedirectStandardError $stderrPath
    $healthy = $false
    $healthOutput = ''
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        if (Test-Path -LiteralPath $statePath -PathType Leaf) {
            $healthOutput = (& $cliBinary healthz --state $statePath 2>&1 | Out-String).Trim()
            if ($LASTEXITCODE -eq 0 -and $healthOutput -eq 'ok') {
                $healthy = $true
                break
            }
        }
        if ($server.HasExited) {
            break
        }
        Start-Sleep -Milliseconds 200
    }
    if (!$healthy) {
        $stderrText = if (Test-Path -LiteralPath $stderrPath) { Get-Content -LiteralPath $stderrPath -Raw } else { '' }
        throw "freshly installed Dropserve did not become healthy: $healthOutput; stderr=$stderrText"
    }
    $state = Get-Content -LiteralPath $statePath -Raw | ConvertFrom-Json
    $port = [int]$state.http_port
    if ($port -lt 1 -or $port -gt 65535) {
        throw "freshly installed Dropserve persisted invalid port $port"
    }

    $remoteURL = "http://${windowsGuestAddress}:${port}/fresh-machine/"
    $remoteBody = (& wsl.exe -d $distro -- wget -T 15 -qO- $remoteURL 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) {
        throw "separate WSL2 guest could not reach $remoteURL`: $remoteBody"
    }
    if ($remoteBody -notmatch 'fresh-machine network proof') {
        throw "separate WSL2 guest received unexpected body from $remoteURL`: $remoteBody"
    }

    $transcript.Add("windows=$([System.Environment]::OSVersion.VersionString)")
    $transcript.Add("installer=$([System.IO.Path]::GetFileName($installerPath))")
    $transcript.Add("guest_kernel=$guestKernel")
    $transcript.Add("guest_route=$($guestRoutes -replace "`r?`n", '; ')")
    $transcript.Add("remote_url=$remoteURL")
    $transcript.Add("remote_body=$remoteBody")

    $uninstaller = Join-Path $installDirectory 'unins000.exe'
    $uninstall = Start-Process -FilePath $uninstaller -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -PassThru -WindowStyle Hidden
    if ($uninstall.ExitCode -ne 0) {
        throw "fresh-machine silent uninstaller exited $($uninstall.ExitCode)"
    }
    if (!$server.WaitForExit(10000)) {
        throw 'fresh-machine uninstaller did not stop Dropserve'
    }
    $server = $null
    $deleteDeadline = [DateTime]::UtcNow.AddSeconds(10)
    while ((Test-Path -LiteralPath $installDirectory) -and [DateTime]::UtcNow -lt $deleteDeadline) {
        Start-Sleep -Milliseconds 100
    }
    if (Test-Path -LiteralPath $installDirectory) {
        throw "fresh-machine uninstaller left $installDirectory"
    }
    & schtasks.exe /Query /TN Dropserve *> $null
    if ($LASTEXITCODE -eq 0) {
        throw 'fresh-machine uninstaller left the Dropserve Scheduled Task'
    }
    $firewallAfter = & netsh.exe advfirewall firewall show rule name=Dropserve 2>&1 | Out-String
    if ($firewallAfter -match 'Rule Name:\s+Dropserve') {
        throw "fresh-machine uninstaller left the Dropserve firewall rule: $firewallAfter"
    }
    $transcript.Add('uninstall=no process, task, firewall rule, or install directory')
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
    if ($distroRegistered) {
        & wsl.exe --terminate $distro 2>$null | Out-Null
        & wsl.exe --unregister $distro 2>$null | Out-Null
    }
    $temporaryRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
    $resolvedSandbox = [System.IO.Path]::GetFullPath($sandbox)
    if ($resolvedSandbox.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -and $resolvedSandbox -ne $temporaryRoot -and (Test-Path -LiteralPath $resolvedSandbox)) {
        [System.IO.Directory]::Delete($resolvedSandbox, $true)
    }
}

Write-Output 'M10 fresh-machine transcript'
$transcript | ForEach-Object { Write-Output $_ }
Write-Output 'guest_cleanup=disposable WSL2 distribution unregistered'
exit 0
