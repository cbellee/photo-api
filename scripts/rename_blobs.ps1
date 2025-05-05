$AZURE_STORAGE_ACCOUNT = 'storysnrxag7pn7z2'
$AZURE_STORAGE_CONTAINER = 'images'
$OLD_NAME = 'Flinders 2025 Part 2'
$NEW_NAME = 'Flinders and Sydney 2024'
$deleteOldBlob = $false
$deleteBlobOnly = $true
$readOnly = $false

# search blobs
$ctx = New-AzStorageContext -StorageAccountName $AZURE_STORAGE_ACCOUNT -UseConnectedAccount

Get-AzStorageBlob -Container $AZURE_STORAGE_CONTAINER -Context $ctx | `
  Where-Object { $_.Name -like "*$OLD_NAME*" } | `
  ForEach-Object {
  if ($deleteBlobOnly) {
    # delete old blob
    Write-Output "Deleting blob '$($_.Name)'"
    Remove-AzStorageBlob -Blob $_.Name -Container $AZURE_STORAGE_CONTAINER -Context $ctx -ConcurrentTaskCount 10
  }
  else {
    # create new blob name
    $new_blob_name = $_.Name -replace $OLD_NAME, $NEW_NAME
    Write-Output "Renaming blob '$($_.Name)' to '$new_blob_name'"

    # get blob tags
    $tags = Get-AzStorageBlobTag -Blob $_.Name -Container $AZURE_STORAGE_CONTAINER -Context $ctx

    # list tags
    Write-Output "Blob tags: $($tags.GetEnumerator())"

    # get blob metadata
    $blob = Get-AzStorageBlob -Blob $_.Name -Container $AZURE_STORAGE_CONTAINER -Context $ctx -IncludeTag 
    $metadata = $blob.BlobClient.GetProperties().Value.Metadata
    Write-Output "Blob metadata: $($metadata.GetEnumerator())"

    if (!$readOnly) {
      Write-Output "Copying blob '$($_.Name)' to: $new_blob_name"
      Copy-AzStorageBlob `
        -SrcContainer $AZURE_STORAGE_CONTAINER `
        -SrcBlob $_.Name `
        -DestContainer $AZURE_STORAGE_CONTAINER `
        -DestBlob $new_blob_name `
        -Context $ctx

      # set blob tags
      $tags['album'] = $NEW_NAME
      $tags['collectionImage'] = 'false'
      $tags['albumImage'] = 'false'

      # set modified tags
      Set-AzStorageBlobTag -Blob $new_blob_name -Container $AZURE_STORAGE_CONTAINER -Context $ctx -Tag $tags

      if ($deleteOldBlob) {
        # delete old blob
        Write-Output "Deleting blob '$($_.Name)'"
        Remove-AzStorageBlob -Blob $_.Name -Container $AZURE_STORAGE_CONTAINER -Context $ctx -ConcurrentTaskCount 10
      }
    }
    else {
      Write-Output "Read only mode. Not copying or deleting blob '$($_.Name)'"
    }
  }
}
