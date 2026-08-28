param(
    [Parameter(Mandatory = $true)]
    [string]$Pfx,
    [Parameter(Mandatory = $true)]
    [string]$Password,
    [Parameter(Mandatory = $true)]
    [string[]]$Files,
    [switch]$SkipTrustVerification
)

$ErrorActionPreference = 'Stop'
$pfxPath = (Resolve-Path -LiteralPath $Pfx).Path
$resolvedFiles = @($Files | ForEach-Object { (Resolve-Path -LiteralPath $_).Path })
$signtool = Get-ChildItem -LiteralPath 'C:\Program Files (x86)\Windows Kits' -Recurse -Filter signtool.exe -ErrorAction SilentlyContinue |
    Where-Object { $_.FullName -match '\\x64\\signtool\.exe$' } |
    Sort-Object FullName -Descending |
    Select-Object -First 1
if ($null -eq $signtool) {
    throw 'signtool.exe was not found in the Windows SDK'
}

$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
$certificate = [System.Security.Cryptography.X509Certificates.X509Certificate2]::new(
    $pfxPath,
    $Password,
    [System.Security.Cryptography.X509Certificates.X509KeyStorageFlags]::EphemeralKeySet
)
$thumbprint = $certificate.Thumbprint
$personalPath = "Cert:\CurrentUser\My\$thumbprint"
$rootPath = "Cert:\LocalMachine\Root\$thumbprint"
$removePersonal = !(Test-Path -LiteralPath $personalPath)
$removeRoot = !$SkipTrustVerification -and !(Test-Path -LiteralPath $rootPath)
$publicCertificate = Join-Path ([System.IO.Path]::GetTempPath()) ("dropserve-signing-$thumbprint.cer")

try {
    if ($removePersonal) {
        Import-PfxCertificate -FilePath $pfxPath -CertStoreLocation Cert:\CurrentUser\My -Password $securePassword -Exportable | Out-Null
    }
    if ($removeRoot) {
        [System.IO.File]::WriteAllBytes(
            $publicCertificate,
            $certificate.Export([System.Security.Cryptography.X509Certificates.X509ContentType]::Cert)
        )
        & certutil.exe -f -addstore Root $publicCertificate | Out-Null
        if ($LASTEXITCODE -ne 0) {
            throw "certutil could not temporarily trust signing certificate $thumbprint"
        }
    }

    foreach ($file in $resolvedFiles) {
        & $signtool.FullName sign /fd SHA256 /sha1 $thumbprint /s My $file
        if ($LASTEXITCODE -ne 0) {
            throw "signtool sign failed for $file with exit code $LASTEXITCODE"
        }
        $embedded = Get-AuthenticodeSignature -LiteralPath $file
        if ($null -eq $embedded.SignerCertificate -or $embedded.SignerCertificate.Thumbprint -ne $thumbprint) {
            throw "signed file does not contain the expected Authenticode certificate: $file"
        }
        if (!$SkipTrustVerification) {
            & $signtool.FullName verify /pa /v $file
            if ($LASTEXITCODE -ne 0) {
                throw "signtool verify /pa failed for $file with exit code $LASTEXITCODE"
            }
        }
        $hash = (Get-FileHash -LiteralPath $file -Algorithm SHA256).Hash.ToLowerInvariant()
        $checksumPath = "$file.sha256"
        [System.IO.File]::WriteAllText($checksumPath, "$hash  $([System.IO.Path]::GetFileName($file))`n", [System.Text.Encoding]::ASCII)
        Write-Output "Signed and verified $([System.IO.Path]::GetFileName($file)); SHA-256 $hash"
    }
}
finally {
    if ($removeRoot -and (Test-Path -LiteralPath $rootPath)) {
        & certutil.exe -delstore Root $thumbprint | Out-Null
    }
    if ($removePersonal -and (Test-Path -LiteralPath $personalPath)) {
        Remove-Item -LiteralPath $personalPath -Force
    }
    if (Test-Path -LiteralPath $publicCertificate) {
        Remove-Item -LiteralPath $publicCertificate -Force
    }
    $certificate.Dispose()
}
