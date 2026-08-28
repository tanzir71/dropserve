param(
    [Parameter(Mandatory = $true)]
    [string[]]$Files,
    [switch]$TrustedVerification
)

$ErrorActionPreference = 'Stop'
$pfx = Join-Path ([System.IO.Path]::GetTempPath()) ("dropserve-signing-test-" + [Guid]::NewGuid().ToString('N') + '.pfx')
$password = [Guid]::NewGuid().ToString('N') + [Guid]::NewGuid().ToString('N')
$thumbprint = ''

try {
    $thumbprint = (& $PSScriptRoot/new-signing-certificate.ps1 -Pfx $pfx -Password $password | Select-Object -Last 1)
    & $PSScriptRoot/sign-windows.ps1 -Pfx $pfx -Password $password -Files $Files -SkipTrustVerification:(-not $TrustedVerification)
    Write-Output "M10 Authenticode smoke passed with self-signed certificate $thumbprint."
}
finally {
    if (Test-Path -LiteralPath $pfx) {
        Remove-Item -LiteralPath $pfx -Force
    }
    if ($thumbprint -match '^[A-Fa-f0-9]{40,64}$') {
        $personal = "Cert:\CurrentUser\My\$thumbprint"
        if (Test-Path -LiteralPath $personal) {
            Remove-Item -LiteralPath $personal -Force
        }
        $trusted = "Cert:\LocalMachine\Root\$thumbprint"
        if ($TrustedVerification -and (Test-Path -LiteralPath $trusted)) {
            & certutil.exe -delstore Root $thumbprint | Out-Null
        }
    }
}
