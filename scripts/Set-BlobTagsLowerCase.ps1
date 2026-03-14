<#
.SYNOPSIS
    Lowercases all blob index tag values in a container.

.DESCRIPTION
    Enumerates every blob in the specified container, reads its index tags,
    and if any tag value contains uppercase characters, rewrites all values
    as lowercase. Unchanged blobs are skipped.

.PARAMETER StorageAccountName
    Name of the storage account.

.PARAMETER ContainerName
    Name of the container whose blob tags should be lowercased.

.PARAMETER DryRun
    If set, reports which blobs would be updated without making changes.

.EXAMPLE
    ./Set-BlobTagsLowerCase.ps1 -StorageAccountName "myaccount" -ContainerName "images"
.EXAMPLE
    ./Set-BlobTagsLowerCase.ps1 -StorageAccountName "myaccount" -ContainerName "images" -DryRun
#>

[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$StorageAccountName,

    [Parameter(Mandatory)]
    [string]$ContainerName,

    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# --- Obtain storage context ---
Write-Host "Getting storage account key..." -ForegroundColor Cyan

$account = Get-AzStorageAccount | Where-Object { $_.StorageAccountName -eq $StorageAccountName }
if (-not $account) { throw "Storage account '$StorageAccountName' not found in current subscription." }

$key = (Get-AzStorageAccountKey -ResourceGroupName $account.ResourceGroupName -Name $StorageAccountName)[0].Value
$ctx = New-AzStorageContext -StorageAccountName $StorageAccountName -StorageAccountKey $key

# --- Enumerate blobs with tags ---
Write-Host "Enumerating blobs in '$StorageAccountName/$ContainerName'..." -ForegroundColor Cyan
$blobs = Get-AzStorageBlob -Container $ContainerName -Context $ctx -IncludeTag

$total = $blobs.Count
$updated = 0
$skipped = 0
$failed = 0

Write-Host "Found $total blob(s)." -ForegroundColor Cyan

foreach ($blob in $blobs) {
    $blobName = $blob.Name
    $tags = $blob.Tags

    if (-not $tags -or $tags.Count -eq 0) {
        $skipped++
        continue
    }

    # Check if any tag value has uppercase characters
    $needsUpdate = $false
    $newTags = @{}
    foreach ($kvp in $tags.GetEnumerator()) {
        $lowerValue = $kvp.Value.ToLower()
        $newTags[$kvp.Key] = $lowerValue
        if ($kvp.Value -cne $lowerValue) {
            $needsUpdate = $true
        }
    }

    if (-not $needsUpdate) {
        $skipped++
        continue
    }

    if ($DryRun) {
        Write-Host "  WOULD UPDATE: $blobName" -ForegroundColor Yellow
        foreach ($kvp in $tags.GetEnumerator()) {
            $lowerValue = $kvp.Value.ToLower()
            if ($kvp.Value -cne $lowerValue) {
                Write-Host "    $($kvp.Key): '$($kvp.Value)' -> '$lowerValue'" -ForegroundColor DarkYellow
            }
        }
        $updated++
        continue
    }

    try {
        $dict = [System.Collections.Generic.Dictionary[string,string]]::new()
        foreach ($k in $newTags.Keys) { $dict[$k] = $newTags[$k] }
        $blob.BlobClient.SetTags($dict) | Out-Null
        Write-Host "  UPDATED: $blobName" -ForegroundColor Green
        $updated++
    }
    catch {
        Write-Warning "  FAILED: $blobName - $_"
        $failed++
    }
}

Write-Host ""
Write-Host "===== Complete =====" -ForegroundColor Green
Write-Host "  Total:   $total"
Write-Host "  Updated: $updated"
Write-Host "  Skipped: $skipped"
Write-Host "  Failed:  $failed"
if ($DryRun) { Write-Host "  (DRY RUN — no changes were made)" -ForegroundColor Yellow }
