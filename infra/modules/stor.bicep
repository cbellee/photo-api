param location string
param name string
param tags object
param containers array
param isPublicBlobAccessAllowed bool = true
param isSupportHttpsTrafficOnly bool = true
param isDefaultToOAuthAuthentication bool = false
param isAllowSharedAccessKey bool = true
param utcValue string = utcNow()
param customDomainName string
param deployCustomDomain bool = false

@allowed([
  'Storage'
  'StorageV2'
  'BlobStorage'
  'BlockBlobStorage'
  'FileStorage'
])
param kind string = 'StorageV2'

@allowed([
  'Standard_LRS'
  'Premium_LRS'
  'Premium_ZRS'
])
param sku string = 'Standard_LRS'

@allowed([
  'Cool'
  'Hot'
  'Premium'
])
param accessTier string = 'Hot'

@allowed([
  'Enabled'
  'Disabled'
])
param isPublicNetworkAccessEnabled string = 'Enabled'

resource storage 'Microsoft.Storage/storageAccounts@2023-05-01' = {
  kind: kind
  location: location
  name: name
  sku: {
    name: sku
  }
  properties: {
    accessTier: accessTier
    allowBlobPublicAccess: isPublicBlobAccessAllowed
    defaultToOAuthAuthentication: isDefaultToOAuthAuthentication
    publicNetworkAccess: isPublicNetworkAccessEnabled
    supportsHttpsTrafficOnly: isSupportHttpsTrafficOnly
    allowSharedKeyAccess: isAllowSharedAccessKey
    customDomain: !deployCustomDomain
      ? null
      : {
          name: customDomainName
        }
  }
  tags: tags
}

resource queueService 'Microsoft.Storage/storageAccounts/queueServices@2023-05-01' = {
  parent: storage
  name: 'default'
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-05-01' = {
  parent: storage
  name: 'default'
}

resource storageQueues 'Microsoft.Storage/storageAccounts/queueServices/queues@2023-05-01' = [
  for container in containers: {
    parent: queueService
    name: container.name
  }
]

resource blobContainers 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-05-01' = [
  for container in containers: {
    parent: blobService
    name: container.name
    properties: {
      publicAccess: container.publicAccess
    }
  }
]

resource enableStaticWebsite 'Microsoft.Resources/deploymentScripts@2020-10-01' = {
  name: 'enableStaticWebsite'
  location: resourceGroup().location
  kind: 'AzureCLI'
  properties: {
    forceUpdateTag: utcValue
    azCliVersion: '2.26.1'
    timeout: 'PT5M'
    retentionInterval: 'PT1H'
    environmentVariables: [
      {
        name: 'AZURE_STORAGE_ACCOUNT'
        value: storage.name
      }
      {
        name: 'AZURE_STORAGE_KEY'
        secureValue: storage.listKeys().keys[0].value
      }
    ]
    arguments: 'index.html'
    scriptContent: 'az storage blob service-properties update --static-website --index-document $1 --404-document $1'
  }
}

output name string = storage.name
output id string = storage.id
output key string = storage.listKeys().keys[0].value
output blobEndpoint string = replace(replace(storage.properties.primaryEndpoints.blob, 'https://', ''), '/', '')
output webEndpoint string = replace(replace(storage.properties.primaryEndpoints.web, 'https://', ''), '/', '')
