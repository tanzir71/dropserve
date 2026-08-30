param(
    [ValidateSet("M7", "M8", "All")]
    [string]$Stage = "All",
    [string]$Binary = "",
    [string]$EvidencePath = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest
$PSNativeCommandUseErrorActionPreference = $false
$ProgressPreference = "SilentlyContinue"

function Add-Evidence {
    param([string]$Line)

    $script:evidence.Add($Line)
}

function Invoke-NativeText {
    param(
        [string]$FilePath,
        [string[]]$Arguments,
        [string]$Label
    )

    $output = @(& $FilePath @Arguments 2>&1)
    $exitCode = $LASTEXITCODE
    $text = $output -join [Environment]::NewLine
    if ($exitCode -ne 0) {
        throw "$Label failed with exit code $exitCode`: $text"
    }
    return $text
}

function Invoke-Dropserve {
    param(
        [string[]]$Arguments,
        [string]$Label
    )

    return Invoke-NativeText -FilePath $script:binaryPath -Arguments $Arguments -Label $Label
}

function Find-Tailscale {
    $command = Get-Command tailscale.exe -ErrorAction SilentlyContinue
    if ($null -ne $command) {
        return $command.Source
    }
    foreach ($candidate in @(
        "C:\Program Files\Tailscale\tailscale.exe",
        "C:\Program Files (x86)\Tailscale\tailscale.exe"
    )) {
        if (Test-Path -LiteralPath $candidate -PathType Leaf) {
            return $candidate
        }
    }
    throw "Tailscale is not installed. Install it, approve Windows elevation, and sign in before running M7."
}

function Get-FreeTCPPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try {
        $listener.Start()
        return ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
    }
    finally {
        $listener.Stop()
    }
}

function Get-TailscaleStatus {
    $raw = Invoke-NativeText -FilePath $script:tailscalePath -Arguments @("status", "--json") -Label "tailscale status --json"
    return $raw | ConvertFrom-Json -ErrorAction Stop
}

function Get-TailscaleServeStatus {
    $raw = Invoke-NativeText -FilePath $script:tailscalePath -Arguments @("serve", "status", "--json") -Label "tailscale serve status --json"
    if ([string]::IsNullOrWhiteSpace($raw)) {
        return [pscustomobject]@{}
    }
    return $raw | ConvertFrom-Json -ErrorAction Stop
}

function Get-ObjectProperties {
    param([object]$Value)

    if ($null -eq $Value) {
        return @()
    }
    return @($Value.PSObject.Properties)
}

function Get-ServeSectionProperties {
    param(
        [object]$Status,
        [string]$Name
    )

    $property = $Status.PSObject.Properties[$Name]
    if ($null -eq $property -or $null -eq $property.Value) {
        return @()
    }
    return @(Get-ObjectProperties -Value $property.Value)
}

function Test-ServeConfigurationEmpty {
    param([object]$Status)

    return @(Get-ServeSectionProperties -Status $Status -Name "Web").Count -eq 0 -and
        @(Get-ServeSectionProperties -Status $Status -Name "TCP").Count -eq 0
}

function Test-ExpectedServeProxy {
    param(
        [object]$Status,
        [string]$ExpectedProxy
    )

    foreach ($webProperty in @(Get-ServeSectionProperties -Status $Status -Name "Web")) {
        $handlersProperty = $webProperty.Value.PSObject.Properties["Handlers"]
        if ($null -eq $handlersProperty -or $null -eq $handlersProperty.Value) {
            continue
        }
        $rootProperty = $handlersProperty.Value.PSObject.Properties["/"]
        if ($null -eq $rootProperty -or $null -eq $rootProperty.Value) {
            continue
        }
        $proxyProperty = $rootProperty.Value.PSObject.Properties["Proxy"]
        if ($null -ne $proxyProperty -and
            $proxyProperty.Value.ToString().TrimEnd("/") -eq $ExpectedProxy.TrimEnd("/")) {
            return $true
        }
    }
    return $false
}

function Get-TrustedRootCount {
    param([string]$Thumbprint)

    return @(Get-ChildItem -LiteralPath "Cert:\CurrentUser\Root" |
        Where-Object { $_.Thumbprint -eq $Thumbprint }).Count
}

function Get-SHA256Text {
    param([string]$Text)

    $algorithm = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($Text)
        return [Convert]::ToHexString($algorithm.ComputeHash($bytes)).ToLowerInvariant()
    }
    finally {
        $algorithm.Dispose()
    }
}

function Wait-Dropserve {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        if ($script:serverProcess.HasExited) {
            $stderr = if (Test-Path -LiteralPath $script:serverStderr) {
                Get-Content -LiteralPath $script:serverStderr -Raw
            }
            else {
                ""
            }
            throw "Dropserve exited before becoming healthy: $stderr"
        }
        if (Test-Path -LiteralPath $script:statePath -PathType Leaf) {
            try {
                $state = Get-Content -LiteralPath $script:statePath -Raw | ConvertFrom-Json -ErrorAction Stop
                $response = Invoke-WebRequest `
                    -Uri "http://127.0.0.1:$($state.http_port)/_dropserve/healthz" `
                    -TimeoutSec 2 `
                    -UseBasicParsing
                if ($response.StatusCode -eq 200 -and $response.Content.Trim() -eq "ok") {
                    return [int]$state.http_port
                }
            }
            catch {
                # The state file and listener become ready independently; retry until the shared deadline.
            }
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Dropserve did not become healthy within 20 seconds."
}

function Wait-RootCertificate {
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    do {
        if (Test-Path -LiteralPath $script:rootCertificatePath -PathType Leaf) {
            return
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Dropserve did not create its isolated local HTTPS root within 20 seconds."
}

function Invoke-M7Acceptance {
    $script:tailscalePath = Find-Tailscale
    $status = Get-TailscaleStatus
    if ($status.BackendState -ne "Running") {
        throw "Tailscale is installed but is not signed in and running (state: $($status.BackendState))."
    }
    $dnsName = $status.Self.DNSName.ToString().TrimEnd(".")
    if ([string]::IsNullOrWhiteSpace($dnsName)) {
        throw "Tailscale is running but did not report a MagicDNS name."
    }

    $before = Get-TailscaleServeStatus
    if (-not (Test-ServeConfigurationEmpty -Status $before)) {
        throw "Refusing to replace a pre-existing Tailscale Serve configuration. Remove it yourself or use another test machine."
    }

    Write-Host "M7 will enable Tailscale Serve exactly once, verify its tailnet-only HTTPS page, and disable it."
    $confirmation = Read-Host "Type SERVE to continue"
    if ($confirmation -cne "SERVE") {
        throw "M7 was cancelled before changing Tailscale Serve."
    }

    $null = Invoke-Dropserve -Arguments @("tailscale", "serve") -Label "dropserve tailscale serve"
    $script:serveEnabledByHarness = $true
    $expectedProxy = "http://127.0.0.1:$($script:httpPort)"
    $enabled = Get-TailscaleServeStatus
    if (-not (Test-ExpectedServeProxy -Status $enabled -ExpectedProxy $expectedProxy)) {
        throw "Tailscale Serve did not publish the expected Dropserve proxy $expectedProxy."
    }

    $tailnetURL = "https://$dnsName/"
    $response = Invoke-WebRequest -Uri $tailnetURL -TimeoutSec 30 -UseBasicParsing
    if ($response.StatusCode -ne 200) {
        throw "Tailnet HTTPS returned $($response.StatusCode), expected 200."
    }

    $null = Invoke-Dropserve -Arguments @("tailscale", "unserve") -Label "dropserve tailscale unserve"
    $script:serveEnabledByHarness = $false
    if (-not (Test-ServeConfigurationEmpty -Status (Get-TailscaleServeStatus))) {
        throw "Tailscale Serve still has configuration after Dropserve disabled it."
    }

    Add-Evidence "- M7 PASS: production Serve published the expected loopback proxy, tailnet HTTPS returned 200, and production unserve restored an empty Serve configuration."
    Add-Evidence "- M7 tailnet host SHA-256: ``$(Get-SHA256Text -Text $dnsName)`` (hostname intentionally not written)."
}

function Invoke-M8Acceptance {
    Wait-RootCertificate
    $certificate = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new($script:rootCertificatePath)
    $thumbprint = $certificate.Thumbprint
    $certificate.Dispose()
    if ((Get-TrustedRootCount -Thumbprint $thumbprint) -ne 0) {
        throw "The isolated root $thumbprint was already trusted before the test."
    }

    Write-Host "M8 will run the shipping trust command. Windows will show its root-certificate security warning."
    Write-Host "Review that warning and approve it only if it names the isolated Dropserve certificate."
    $confirmation = Read-Host "Type TRUST to continue"
    if ($confirmation -cne "TRUST") {
        throw "M8 was cancelled before changing the Windows trust store."
    }

    $null = Invoke-Dropserve -Arguments @("trust", "install") -Label "dropserve trust install"
    $script:trustInstalledByHarness = $true
    $installedCount = Get-TrustedRootCount -Thumbprint $thumbprint
    if ($installedCount -ne 1) {
        throw "Windows contains $installedCount copies of $thumbprint after install, expected exactly one."
    }

    $response = Invoke-WebRequest -Uri "https://localhost:$($script:httpsPort)/" -TimeoutSec 20 -UseBasicParsing
    if ($response.StatusCode -ne 200) {
        throw "Trusted local HTTPS returned $($response.StatusCode), expected 200."
    }

    $null = Invoke-Dropserve -Arguments @("trust", "uninstall") -Label "dropserve trust uninstall"
    $script:trustInstalledByHarness = $false
    $remainingCount = Get-TrustedRootCount -Thumbprint $thumbprint
    if ($remainingCount -ne 0) {
        throw "Windows still contains $remainingCount copies of $thumbprint after uninstall."
    }

    Add-Evidence "- M8 PASS: production trust install created exactly one trusted root, system-trusted local HTTPS returned 200, and production trust uninstall returned the thumbprint count to zero."
    Add-Evidence "- M8 isolated root thumbprint: ``$thumbprint``."
}

if ($env:OS -ne "Windows_NT") {
    throw "This acceptance harness must run on real Windows."
}
if (-not [Environment]::UserInteractive) {
    throw "This acceptance harness requires an interactive human session."
}

$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if ($Binary -eq "") {
    $Binary = Join-Path $repositoryRoot "bin\dropserve-cli.exe"
}
$script:binaryPath = (Resolve-Path -LiteralPath $Binary -ErrorAction Stop).Path

$temporaryRoot = [System.IO.Path]::GetFullPath([System.IO.Path]::GetTempPath())
$script:workspace = Join-Path $temporaryRoot ("dropserve-human-acceptance-" + [Guid]::NewGuid().ToString("N"))
$script:workspace = [System.IO.Path]::GetFullPath($script:workspace)
if (-not $script:workspace.StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase) -or
    (Split-Path -Leaf $script:workspace) -notlike "dropserve-human-acceptance-*") {
    throw "Refusing to use an acceptance workspace outside the Windows temporary directory."
}
if ($EvidencePath -eq "") {
    $EvidencePath = Join-Path $temporaryRoot ("dropserve-v1-human-evidence-" + [DateTime]::UtcNow.ToString("yyyyMMddTHHmmssZ") + ".md")
}
$EvidencePath = [System.IO.Path]::GetFullPath($EvidencePath)
if ($EvidencePath.StartsWith($script:workspace + [System.IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "EvidencePath must be outside the disposable acceptance workspace."
}

$script:evidence = [System.Collections.Generic.List[string]]::new()
Add-Evidence "# Dropserve v1 human acceptance evidence"
Add-Evidence ""
Add-Evidence "- Evidence transcript created: $([DateTime]::UtcNow.ToString("o"))"
Add-Evidence "- Stage: $Stage"
Add-Evidence "- Windows: $([Environment]::OSVersion.VersionString)"
Add-Evidence "- Dropserve binary SHA-256: ``$((Get-FileHash -LiteralPath $script:binaryPath -Algorithm SHA256).Hash.ToLowerInvariant())``"

$oldLocalAppData = $env:LOCALAPPDATA
$oldAppData = $env:APPDATA
$script:serverProcess = $null
$script:serveEnabledByHarness = $false
$script:trustInstalledByHarness = $false
$script:tailscalePath = ""
$script:httpPort = Get-FreeTCPPort
$script:httpsPort = if ($Stage -in @("M8", "All")) { Get-FreeTCPPort } else { 0 }
$localAppData = Join-Path $script:workspace "LocalAppData"
$roamingAppData = Join-Path $script:workspace "RoamingAppData"
$appsRoot = Join-Path $script:workspace "Apps"
$script:statePath = Join-Path $localAppData "Dropserve\state.json"
$configPath = Join-Path $roamingAppData "Dropserve\config.toml"
$script:rootCertificatePath = Join-Path (Split-Path -Parent $script:statePath) "ca\root.pem"
$script:serverStdout = Join-Path $script:workspace "dropserve.stdout.log"
$script:serverStderr = Join-Path $script:workspace "dropserve.stderr.log"

try {
    New-Item -ItemType Directory -Path $appsRoot -Force | Out-Null
    New-Item -ItemType Directory -Path (Split-Path -Parent $configPath) -Force | Out-Null
    [System.IO.File]::WriteAllText(
        (Join-Path $appsRoot "index.html"),
        "<!doctype html><title>Dropserve human acceptance</title><h1>Dropserve human acceptance</h1>",
        [System.Text.UTF8Encoding]::new($false)
    )
    $appsTOML = $appsRoot.Replace("\", "/")
    $configText = @"
[server]
apps_roots = ["$appsTOML"]
http_port = $($script:httpPort)
https_port = $($script:httpsPort)
bind = "127.0.0.1"
app_port_range = [7400, 7999]

[discovery]
mdns = false
mdns_name = "dropserve"
tailscale = true

[updates]
check = false
"@
    [System.IO.File]::WriteAllText($configPath, $configText, [System.Text.UTF8Encoding]::new($false))

    $env:LOCALAPPDATA = $localAppData
    $env:APPDATA = $roamingAppData
    $serverArguments = @(
        "serve",
        "--config", ('"' + $configPath.Replace('"', '\"') + '"'),
        "--state", ('"' + $script:statePath.Replace('"', '\"') + '"'),
        "--root", ('"' + $appsRoot.Replace('"', '\"') + '"')
    ) -join " "
    $script:serverProcess = Start-Process `
        -FilePath $script:binaryPath `
        -ArgumentList $serverArguments `
        -RedirectStandardOutput $script:serverStdout `
        -RedirectStandardError $script:serverStderr `
        -WindowStyle Hidden `
        -PassThru
    $script:httpPort = Wait-Dropserve

    if ($Stage -in @("M7", "All")) {
        Invoke-M7Acceptance
    }
    if ($Stage -in @("M8", "All")) {
        Invoke-M8Acceptance
    }
    Add-Evidence "- Overall result: PASS"
}
catch {
    Add-Evidence "- Overall result: FAIL — $($_.Exception.Message)"
    throw
}
finally {
    if ($script:serveEnabledByHarness -and $null -ne $script:serverProcess -and -not $script:serverProcess.HasExited) {
        try {
            $null = Invoke-Dropserve -Arguments @("tailscale", "unserve") -Label "dropserve tailscale unserve cleanup"
            Add-Evidence "- Cleanup: removed the Tailscale Serve mapping enabled by this harness."
        }
        catch {
            Add-Evidence "- CLEANUP REQUIRED: Tailscale Serve cleanup failed — $($_.Exception.Message)"
        }
    }
    if ($script:trustInstalledByHarness) {
        try {
            $null = Invoke-Dropserve -Arguments @("trust", "uninstall") -Label "dropserve trust uninstall cleanup"
            Add-Evidence "- Cleanup: removed the isolated root installed by this harness."
        }
        catch {
            Add-Evidence "- CLEANUP REQUIRED: trust uninstall failed — $($_.Exception.Message)"
        }
    }
    if ($null -ne $script:serverProcess -and -not $script:serverProcess.HasExited) {
        Stop-Process -Id $script:serverProcess.Id -Force -ErrorAction SilentlyContinue
        $script:serverProcess.WaitForExit(5000) | Out-Null
    }
    $env:LOCALAPPDATA = $oldLocalAppData
    $env:APPDATA = $oldAppData

    $evidenceDirectory = Split-Path -Parent $EvidencePath
    if ($evidenceDirectory -ne "") {
        New-Item -ItemType Directory -Path $evidenceDirectory -Force | Out-Null
    }
    [System.IO.File]::WriteAllLines($EvidencePath, $script:evidence, [System.Text.UTF8Encoding]::new($false))
    Write-Host "Evidence transcript: $EvidencePath"

    if (Test-Path -LiteralPath $script:workspace) {
        Remove-Item -LiteralPath $script:workspace -Recurse -Force
    }
}
