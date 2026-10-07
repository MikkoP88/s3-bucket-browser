# trust-cert.ps1 - make the self-signed "s3b Project" build trusted on one
# machine. Chain trust lives ONLY in Trusted Root Certification Authorities;
# Trusted Publisher (added on top) then silences the "unknown publisher"
# prompt for code signed by it. Importing into Trusted People or Trusted
# Publisher alone does NOT resolve the chain - the certificate keeps reading
# "this root certificate is not trusted".
#
# Usage:
#   powershell -ExecutionPolicy Bypass -File scripts\trust-cert.ps1                 # machine-wide (needs an elevated shell)
#   powershell -ExecutionPolicy Bypass -File scripts\trust-cert.ps1 -CurrentUser    # this user only
#   powershell -ExecutionPolicy Bypass -File scripts\trust-cert.ps1 -Remove         # undo (combine with -CurrentUser)
param(
    [switch]$CurrentUser,
    [switch]$Remove
)

$ErrorActionPreference = 'Stop'

$certPath = Join-Path $PSScriptRoot 'certs\s3b-signing.cer'
if (-not (Test-Path $certPath)) {
    Write-Error "certificate not found: $certPath"
}

$rootStore = if ($CurrentUser) { 'Cert:\CurrentUser\Root' } else { 'Cert:\LocalMachine\Root' }
$pubStore = if ($CurrentUser) { 'Cert:\CurrentUser\TrustedPublisher' } else { 'Cert:\LocalMachine\TrustedPublisher' }

if (-not $CurrentUser) {
    $principal = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Error "machine-wide trust needs an elevated shell (Run as Administrator), or pass -CurrentUser"
    }
}

$cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2 $certPath

function Remove-S3bCert($storePath) {
    Get-ChildItem $storePath -ErrorAction SilentlyContinue |
        Where-Object { $_.Thumbprint -eq $cert.Thumbprint } |
        Remove-Item
}

if ($Remove) {
    Remove-S3bCert $rootStore
    Remove-S3bCert $pubStore
    Write-Host "removed the s3b signing certificate from $rootStore and $pubStore"
    exit 0
}

# Root = chain trust (the store the 'not trusted' warning reads).
Import-Certificate -FilePath $certPath -CertStoreLocation $rootStore | Out-Null
# Trusted Publisher = no 'unknown publisher' prompt for s3b binaries.
Import-Certificate -FilePath $certPath -CertStoreLocation $pubStore | Out-Null

Write-Host "imported the s3b signing certificate into $rootStore and $pubStore"
Write-Host "already-open windows may need a restart before they pick up the new trust"
