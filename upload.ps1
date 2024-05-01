function Set-Image {

    param(
        $imageFullName,
        $collection,
        $album,
        $name,
        $storageAccountName,
        $containerName,
        $isCollectionAlbumImage
    )

    $blobFullName = "$collection/$album/$name"
    
    "Collection: $collection"
    "Album: $album"
    "Width: $($image.Width)"
    "Height: $($image.Height)"
    "Name: $blobFullName"

    $mimeType = ''

    Add-Type -AssemblyName System.Drawing
    $image = New-Object System.Drawing.Bitmap $imageFullName
    $ratio = $($image.Width)/$($image.Height)
    $roundedRatio = [math]::Round($ratio, 2)

    $decoders = [System.Drawing.Imaging.ImageCodecInfo]::GetImageDecoders()
    foreach($decoder in $decoders) {
        if ($image.RawFormat.Guid -eq $decoder.FormatID) {
            $mimeType = $decoder.mimeType
        }
    }

    az storage blob upload `
        --account-name $storageAccountName `
        --container-name $containerName `
        --name $blobFullName `
        --auth-mode login `
        --file $imageFullName `
        --content-type $mimeType `
        --metadata Width=$($image.Width) Height=$($image.Height) Name=$blobFullName Ratio=$roundedRatio `
        --tags Collection=$collection Album=$album Name=$blobFullName CollectionImage=$isCollectionAlbumImage `
        --overwrite
}

function Add-Blobs {

    param
    (
        $StorageAccountName = "storqra2f23aqljtm",
        $RootDirectory = "./images",
        $ContainerName = "uploads"
    )

     $imageFormats = @{}
     $imageFormats['B96B3CAB-0728-11D3-9D7B-0000F81EF32E'] = 'image/bitmap'
     $imageFormats['B96B3CAF-0728-11D3-9D7B-0000F81EF32E'] = 'image/png'
     $imageFormats['B96B3CB0-0728-11D3-9D7B-0000F81EF32E'] = 'image/gif'
     $imageFormats['B96B3CAE-0728-11D3-9D7B-0000F81EF32E'] = 'image/jpeg'
     $imageFormats['B96B3CB1-0728-11D3-9D7B-0000F81EF32E'] = 'image/tiff'

     $i = 0

    Get-ChildItem -Path $RootDirectory -Directory | ForEach-Object {
        $collection = $_.Name
        $collection

        Get-ChildItem -Path $RootDirectory/$collection -Directory | ForEach-Object {
            $album = $_.Name
            "$collection : $album"
            $i = 0

            Get-ChildItem -Path $RootDirectory/$collection/$album -File | ForEach-Object {
                if ($i -eq 0) {
                    $isCollectionAlbumImage = 'true'
                } else {
                    $isCollectionAlbumImage = 'false'
                }

                Set-Image -imageFullName $_.FullName `
                    -collection $collection `
                    -album $album `
                    -name $_.Name `
                    -storageAccountName $StorageAccountName `
                    -containerName $ContainerName `
                    -isCollectionAlbumImage $isCollectionAlbumImage

                $i ++
            }
        }
    }
}

function Remove-Blobs {
    param
    (
        $StorageAccountName = "storqra2f23aqljtm",
        $Containers = @('uploads', 'images', 'thumbs')
    )

    foreach ($container in $Containers) {
        "Removing Blobs in container: $container"
        az storage blob delete-batch -s $container --account-name $StorageAccountName
    }
}
