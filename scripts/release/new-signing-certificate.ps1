param(
    [Parameter(Mandatory = $true)]
    [string]$Pfx,
    [Parameter(Mandatory = $true)]
    [string]$Password
)

$ErrorActionPreference = 'Stop'
$pfxPath = [System.IO.Path]::GetFullPath($Pfx)
$parent = [System.IO.Path]::GetDirectoryName($pfxPath)
if (!(Test-Path -LiteralPath $parent)) {
    New-Item -ItemType Directory -Path $parent | Out-Null
}
if (Test-Path -LiteralPath $pfxPath) {
    throw "refusing to overwrite existing signing key: $pfxPath"
}

$certificate = New-SelfSignedCertificate `
    -Type CodeSigningCert `
    -Subject 'CN=Dropserve Open Source Project' `
    -FriendlyName 'Dropserve self-signed release signing' `
    -CertStoreLocation Cert:\CurrentUser\My `
    -KeyAlgorithm RSA `
    -KeyLength 3072 `
    -HashAlgorithm SHA256 `
    -KeyExportPolicy Exportable `
    -NotAfter ([DateTime]::UtcNow.AddYears(10))
$securePassword = ConvertTo-SecureString -String $Password -AsPlainText -Force
Export-PfxCertificate -Cert $certificate -FilePath $pfxPath -Password $securePassword -CryptoAlgorithmOption AES256_SHA256 | Out-Null
Write-Output $certificate.Thumbprint
