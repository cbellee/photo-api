<#
.SYNOPSIS
    Copies an entire container (with tags and metadata) between storage accounts
    using azcopy server-to-server copy (no local download).

.DESCRIPTION
    Generates SAS tokens for both accounts and uses azcopy to bulk-copy the
    whole container in a single operation. Blob metadata, properties, and tags
    are all preserved. Both storage accounts must be in the same subscription
    and accessible via the current Az context.

    Requires azcopy to be installed and on PATH.

.PARAMETER SourceAccountName
    Name of the source storage account.

.PARAMETER DestAccountName
    Name of the destination storage account.

.PARAMETER ContainerName
    Name of the container (same name used in both accounts).

.PARAMETER OverwriteExisting
    If set, overwrites blobs that already exist in the destination.
    Default behaviour skips blobs that already exist (--overwrite=false).

.EXAMPLE
    ./Copy-Blobs.ps1 -SourceAccountName "srcaccount" -DestAccountName "destaccount" -ContainerName "images"
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$SourceAccountName,

    [Parameter(Mandatory)]
    [string]$DestAccountName,

    [Parameter(Mandatory)]
    [string]$ContainerName,

    [switch]$OverwriteExisting
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# --- Verify azcopy is available ---
if (-not (Get-Command azcopy -ErrorAction SilentlyContinue)) {
    throw "azcopy is not installed or not on PATH. Install from https://aka.ms/azcopy"
}

# --- Obtain storage contexts ---
Write-Host "Getting storage account keys..." -ForegroundColor Cyan

$srcAccount = Get-AzStorageAccount | Where-Object { $_.StorageAccountName -eq $SourceAccountName }
$destAccount = Get-AzStorageAccount | Where-Object { $_.StorageAccountName -eq $DestAccountName }

if (-not $srcAccount) { throw "Source storage account '$SourceAccountName' not found in current subscription." }
if (-not $destAccount) { throw "Destination storage account '$DestAccountName' not found in current subscription." }

$srcKey = (Get-AzStorageAccountKey -ResourceGroupName $srcAccount.ResourceGroupName -Name $SourceAccountName)[0].Value
$destKey = (Get-AzStorageAccountKey -ResourceGroupName $destAccount.ResourceGroupName -Name $DestAccountName)[0].Value

$srcCtx = New-AzStorageContext -StorageAccountName $SourceAccountName -StorageAccountKey $srcKey
$destCtx = New-AzStorageContext -StorageAccountName $DestAccountName -StorageAccountKey $destKey

# --- Ensure destination container exists ---
$destContainer = Get-AzStorageContainer -Name $ContainerName -Context $destCtx -ErrorAction SilentlyContinue
if (-not $destContainer) {
    Write-Host "Creating destination container '$ContainerName'..." -ForegroundColor Yellow
    New-AzStorageContainer -Name $ContainerName -Context $destCtx -Permission Off | Out-Null
}

# --- Generate SAS tokens for both containers ---
$sasExpiry = (Get-Date).AddHours(4)

$srcSas = New-AzStorageContainerSASToken `
    -Name $ContainerName `
    -Context $srcCtx `
    -Permission rlt `
    -ExpiryTime $sasExpiry

$destSas = New-AzStorageContainerSASToken `
    -Name $ContainerName `
    -Context $destCtx `
    -Permission rwlt `
    -ExpiryTime $sasExpiry

"`$srcSas: $srcSas"
"`$destSas: $destSas"

# --- Build source and destination URLs ---
$srcUrl = "$($srcCtx.BlobEndPoint.TrimEnd('/'))/$($ContainerName)?$($srcSas)"
$destUrl = "$($destCtx.BlobEndPoint.TrimEnd('/'))/$($ContainerName)?$($destSas)"

"`$srcUrl: $srcUrl"
"`$destUrl: destUrl"

# --- Run azcopy ---
$overwrite = if ($OverwriteExisting) { 'true' } else { 'false' }

# Force azcopy to use the SAS tokens in the URLs rather than any stale cached AAD login
$env:AZCOPY_AUTO_LOGIN_TYPE = ''

Write-Host "Starting azcopy server-to-server copy..." -ForegroundColor Cyan
Write-Host "  Source:      $SourceAccountName/$ContainerName" -ForegroundColor White
Write-Host "  Destination: $DestAccountName/$ContainerName" -ForegroundColor White

azcopy logout 2>$null

azcopy copy $srcUrl $destUrl `
    --recursive `
    --s2s-preserve-properties `
    --s2s-preserve-access-tier `
    --s2s-preserve-blob-tags `
    --overwrite=$overwrite

if ($LASTEXITCODE -ne 0) {
    throw "azcopy exited with code $LASTEXITCODE"
}

Write-Host ""
Write-Host "===== Copy complete =====" -ForegroundColor Green
