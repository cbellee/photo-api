$storageAccountName = 'storysnrxag7pn7z2'
$container = 'images'
$ctx = New-AzStorageContext -StorageAccountName $storageAccountName
$blobs = Get-AzStorageBlobByTag -TagFilterSqlExpression """collection""='travel'" -Container images -Context $ctx
$blobs | ForEach-Object {
    $tags = Get-AzStorageBlobTag -Context $ctx -Container $container -Blob $_.name
    $tags['collection']='Travel'
    Set-AzStorageBlobTag -Context $ctx -Blob $_.name -Container $container -Tag $tags
}


#westus2
#switzerlandnorth
#eastus
#westeurope
#northeurope
#norwayeast