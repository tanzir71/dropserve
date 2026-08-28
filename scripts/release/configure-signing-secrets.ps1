param(
    [string]$Repository = 'tanzir71/dropserve'
)

$ErrorActionPreference = 'Stop'
$pfx = Join-Path ([System.IO.Path]::GetTempPath()) ("dropserve-release-signing-" + [Guid]::NewGuid().ToString('N') + '.pfx')
$password = [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(48))
$thumbprint = ''

try {
    $thumbprint = (& $PSScriptRoot/new-signing-certificate.ps1 -Pfx $pfx -Password $password | Select-Object -Last 1)
    $pfxBase64 = [Convert]::ToBase64String([System.IO.File]::ReadAllBytes($pfx))
    $pfxBase64 | gh secret set WINDOWS_SIGNING_PFX_BASE64 --repo $Repository
    if ($LASTEXITCODE -ne 0) { throw 'could not store WINDOWS_SIGNING_PFX_BASE64' }
    $password | gh secret set WINDOWS_SIGNING_PASSWORD --repo $Repository
    if ($LASTEXITCODE -ne 0) { throw 'could not store WINDOWS_SIGNING_PASSWORD' }
    Write-Output "Configured encrypted Windows signing secrets for $Repository with certificate $thumbprint."
}
finally {
    $pfxBase64 = $null
    $password = $null
    if (Test-Path -LiteralPath $pfx) {
        Remove-Item -LiteralPath $pfx -Force
    }
    if ($thumbprint -match '^[A-Fa-f0-9]{40,64}$') {
        & certutil.exe -user -delstore My $thumbprint | Out-Null
    }
}
