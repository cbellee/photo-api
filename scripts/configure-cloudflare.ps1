$appSecret = ''
$resourceGroupName = 'photo-app-uksouth-rg'
$storageAccountName = 'storysnrxag7pn7z2'
$TenantId = 'dd3cd82b-4ff1-4e7e-9220-1a9879b8d596'
$ApplicationId = '3aa5caa4-3c25-43e7-86d6-d0405e4a3a04'
$cNameRecord = 'photo'
$zoneName = 'bellee.net'

$SecurePassword = ConvertTo-SecureString -String $appSecret -AsPlainText -Force
$Credential = New-Object -TypeName System.Management.Automation.PSCredential -ArgumentList $ApplicationId, $SecurePassword

Connect-AzAccount -ServicePrincipal -TenantId $TenantId -Credential $Credential

$storageAccount = Get-AzStorageAccount -ResourceGroupName $resourceGroupName -Name $storageAccountName
$storageAccount.CustomDomain = New-Object Microsoft.Azure.Management.Storage.Models.CustomDomain
$storageAccount.CustomDomain.Name = "$cNameRecord.$zoneName"
$storageAccount.CustomDomain.UseSubDomain = $true

$alias = $null

while ($null -eq $alias) {
    $alias = [system.net.dns]::GetHostEntry("$cNameRecord.$zoneName").Aliases
    Start-Sleep -Seconds 5
}

$alias

Set-AzStorageAccount -CustomDomain $storageAccount.CustomDomain -ResourceGroupName $resourceGroupName -Name $storageAccountName
