$storageAccountName = 'storysnrxag7pn7z2'
$containerName = 'images'


# search blobs
$ctx = New-AzStorageContext -StorageAccountName $storageAccountName -UseConnectedAccount

Get-AzStorageBlob -Container $containerName -Context $ctx -Delimiter "/" | ForEach-Object {
    if ($_.BlobType -eq "BlockBlob") {
        # This is a file
        Write-Host "File: $($_.Name)"
    } elseif ($_.BlobType -eq "Directory") {
        # This is a virtual directory (based on the delimiter)
        Write-Host "Directory: $($_.Name)"
    }
}