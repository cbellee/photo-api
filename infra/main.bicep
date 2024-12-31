param photoApiContainerImage string
param resizeApiContainerImage string
param cpuResource string = '0.25'
param memoryResource string = '0.5Gi'
param zoneName string = 'bellee.net'
param cNameRecord string = 'photo'
// param dnsResourceGroupName string = 'external-domain-rg'
param ghcrName string = 'ghcr.io'
param githubUsername string = 'cbellee'
param utcValue string = utcNow()
param cloudFlareZoneId string
param cloudFlareEmail string
param cloudFlareApiKey string
param cloudFlareApiToken string

@secure()
param ghcrPullToken string

param tags object = {
  Environment: 'Dev'
  Role: 'Deployment'
}

@allowed([
  'Microsoft.Storage.BlobCreated'
  'Microsoft.Storage.BlobDeleted'
  'Microsoft.Storage.BlobTierChanged'
  'Microsoft.Storage.AsyncOperationInitiated'
])
param eventTypes array = [
  'Microsoft.Storage.BlobCreated'
]

param containers array = [
  {
    name: 'uploads'
    publicAccess: 'None'
  }
  {
    name: 'images'
    publicAccess: 'Blob'
  }
]

param photoApiName string = 'photo'
param photoApiPort string = '80'
param resizeApiName string = 'resize'
param resizeApiPort string = '80'
param grpcMaxRequestSizeMb int = 50
param maxThumbHeight string = '300'
param maxThumbWidth string = '300'
param maxImageHeight string = '1200'
param maxImageWidth string = '1600'
param uploadsStorageQueueName string = 'uploads'
param imagesStorageQueueName string = 'images'
param imagesContainerName string = 'images'
param uploadsContainerName string = 'uploads'

var storageBlobDataOwnerRoleDefinitionID = 'b7e6dc6d-f1e8-4753-8033-0f276bb0955b'
// var acrPullRoleDefinitionId = '7f951dda-4ed3-4680-a7ca-43fe172d538d'
var storageKey = storage.outputs.key
var storageQueueCxnString = 'DefaultEndpointsProtocol=https;AccountName=${storage.outputs.name};EndpointSuffix=${environment().suffixes.storage};AccountKey=${storageKey}'
var affix = uniqueString(resourceGroup().id)
var umidName = 'umid-${affix}'
var workspaceName = 'wks-${affix}'
var storageAccountName = 'stor${affix}'
var topicName = 'egt-${affix}'
var containerAppEnvName = 'appenv-${affix}'
// var cdnEndpoint = 'cdne-${affix}.azureedge.net'
var cName = '${cNameRecord}.${zoneName}'
var corsOrigins = 'http://localhost:5173,https://${cName}'

// extension microsoftGraphV1

targetScope = 'resourceGroup'

/* resource applicationRegistration 'Microsoft.Graph/applications@v1.0' = {
  displayName: 'photo-app-${affix}'
  uniqueName: 'photo-app-${affix}'
  signInAudience: 'AzureADMyOrg'
  spa: {
    redirectUris: [
      'https://localhost:5173'
      'https://${cName}.${zoneName}'
    ]
  }
appRoles: [
  {
    allowedMemberTypes: [
      'User'
      'Application'
    ]
    description: 'Upload access to the photo app'
    displayName: 'photo.upload'
    value: 'photo.upload'
  }
]
identifierUris: [
  'api://'
]
} */

/* resource acr 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' existing = {
  name: acrName
} */

resource umid 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-07-31-preview' = {
  name: umidName
  location: resourceGroup().location
  tags: tags
}

module storage './modules/stor.bicep' = {
  name: 'StorageDeployment'
  params: {
    kind: 'StorageV2'
    location: resourceGroup().location
    name: storageAccountName
    tags: tags
    containers: containers
    sku: 'Standard_LRS'
    customDomainName: cName
  }
}

module workspace 'br/public:avm/res/operational-insights/workspace:0.3.4' = {
  name: 'WorkspaceDeployment'
  params: {
    name: workspaceName
    tags: tags
  }
}

/* resource storage 'Microsoft.Storage/storageAccounts@2023-04-01' = {
  name: storageAccountName
  location: resourceGroup().location
  tags: tags
  sku: {
    name: 'Standard_LRS'
  }
  kind: 'StorageV2'
}

resource blobService 'Microsoft.Storage/storageAccounts/blobServices@2023-04-01' = {
  parent: storage
  name: 'default'
}

resource blobContainer 'Microsoft.Storage/storageAccounts/blobServices/containers@2023-04-01' = [
  for container in containers: {
    parent: blobService
    name: container.name
    properties: {
      publicAccess: container.publicAccess
    }
  }
]

resource queueService 'Microsoft.Storage/storageAccounts/queueServices@2023-04-01' = {
  parent: storage
  name: 'default'
}

resource storageQueues 'Microsoft.Storage/storageAccounts/queueServices/queues@2023-01-01' = [
  for container in containers: {
    parent: queueService
    name: container.name
  }
] */

@batchSize(1)
module egt 'br/public:avm/res/event-grid/system-topic:0.2.6' = [
  for container in containers: {
    name: 'EventGridDeployment-${container.name}'
    params: {
      tags: tags
      name: topicName
      source: storage.outputs.id
      topicType: 'Microsoft.Storage.StorageAccounts'
      eventSubscriptions: [
        {
          name: container.name
          destination: {
            endpointType: 'StorageQueue'
            properties: {
              queueName: container.name
              queueMessageTimeToLiveInSeconds: 600
              resourceId: storage.outputs.id
            }
          }
          eventDeliverySchema: 'EventGridSchema'
          retryPolicy: {
            eventTimeToLiveInMinutes: 5
            maxDeliveryAttempts: 10
          }
          filter: {
            subjectBeginsWith: '/blobServices/default/containers/${container.name}/'
            includedEventTypes: eventTypes
            enableAdvancedFilteringOnArrays: false
          }
        }
      ]
    }
  }
]

module containerAppEnvironment 'br/public:avm/res/app/managed-environment:0.4.5' = {
  name: 'ContainerAppEnvironmentDeployment'
  params: {
    name: containerAppEnvName
    logAnalyticsWorkspaceResourceId: workspace.outputs.resourceId
    internal: false
    tags: tags
    zoneRedundant: false
  }
}

resource storageRbac 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(umid.name, 'storageDataContributor', affix)
  properties: {
    principalId: umid.properties.principalId
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', storageBlobDataOwnerRoleDefinitionID)
    principalType: 'ServicePrincipal'
  }
}

resource resizeApi 'Microsoft.App/containerApps@2024-08-02-preview' = {
  name: resizeApiName
  location: resourceGroup().location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
  dependsOn: [
    egt
  ]
  properties: {
    configuration: {
      activeRevisionsMode: 'single'
      dapr: {
        appId: resizeApiName
        appPort: int(resizeApiPort)
        appProtocol: 'grpc'
        enabled: true
        httpMaxRequestSize: grpcMaxRequestSizeMb
      }
      secrets: [
        {
          name: 'storage-queue-cxn'
          value: storageQueueCxnString
        }
        {
          name: 'ghcr-pull-token'
          value: ghcrPullToken
        }
      ]
      registries: [
        {
          server: ghcrName
          username: githubUsername
          passwordSecretRef: 'ghcr-pull-token'
        }
      ]
      ingress: {
        external: false
        targetPort: int(resizeApiPort)
        traffic: [
          {
            latestRevision: true
            weight: 100
          }
        ]
        transport: 'http'
      }
    }
    managedEnvironmentId: containerAppEnvironment.outputs.resourceId
    template: {
      containers: [
        {
          image: resizeApiContainerImage
          name: resizeApiName
          resources: {
            cpu: cpuResource
            memory: memoryResource
          }
          env: [
            {
              name: 'SERVICE_NAME'
              value: resizeApiName
            }
            {
              name: 'SERVICE_PORT'
              value: resizeApiPort
            }
            {
              name: 'UPLOADS_QUEUE_BINDING'
              value: 'queue-${uploadsContainerName}'
            }
            {
              name: 'IMAGES_CONTAINER_BINDING'
              value: 'blob-${imagesContainerName}'
            }
            {
              name: 'UPLOADS_CONTAINER_BINDING'
              value: 'blob-${uploadsContainerName}'
            }
            {
              name: 'MAX_THUMB_HEIGHT'
              value: maxThumbHeight
            }
            {
              name: 'MAX_THUMB_WIDTH'
              value: maxThumbWidth
            }
            {
              name: 'MAX_IMAGE_HEIGHT'
              value: maxImageHeight
            }
            {
              name: 'MAX_IMAGE_WIDTH'
              value: maxImageWidth
            }
            {
              name: 'GRPC_MAX_REQUEST_BODY_SIZE_MB'
              value: string(grpcMaxRequestSizeMb)
            }
            {
              name: 'STORAGE_ACCOUNT_NAME'
              value: storage.outputs.name
            }
            {
              name: 'STORAGE_ACCOUNT_SUFFIX'
              value: 'blob.${environment().suffixes.storage}'
            }
            {
              name: 'AZURE_CLIENT_ID'
              value: umid.properties.clientId
            }
            {
              name: 'AZURE_TENANT_ID'
              value: tenant().tenantId
            }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 2
        rules: [
          {
            name: 'azure-queue-scaler'
            azureQueue: {
              queueLength: 5
              queueName: 'uploads'
              auth: [
                {
                  secretRef: 'storage-queue-cxn'
                  triggerParameter: 'connection'
                }
              ]
            }
          }
        ]
      }
    }
  }
}

resource photoApi 'Microsoft.App/containerApps@2023-11-02-preview' = {
  name: photoApiName
  location: resourceGroup().location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
  dependsOn: [
    egt
  ]
  properties: {
    configuration: {
      activeRevisionsMode: 'single'
      secrets: [
        {
          name: 'ghcr-pull-token'
          value: ghcrPullToken
        }
      ]
      registries: [
        {
          server: ghcrName
          username: githubUsername
          passwordSecretRef: 'ghcr-pull-token'
        }
      ]
      ingress: {
        corsPolicy: {
          allowedOrigins: [
            'http://localhost:3000'
            'http://localhost:5173'
            'https://${storage.outputs.webEndpoint}'
            'https://${cName}.${zoneName}'
          ]
          allowedHeaders: [
            '*'
          ]
          allowedMethods: [
            '*'
          ]
          exposeHeaders: [
            '*'
          ]
        }
        external: true
        targetPort: int(photoApiPort)
        traffic: [
          {
            latestRevision: true
            weight: 100
          }
        ]
        transport: 'http'
      }
    }
    managedEnvironmentId: containerAppEnvironment.outputs.resourceId
    template: {
      containers: [
        {
          image: photoApiContainerImage
          name: photoApiName
          resources: {
            cpu: cpuResource
            memory: memoryResource
          }
          env: [
            {
              name: 'SERVICE_NAME'
              value: photoApiName
            }
            {
              name: 'SERVICE_PORT'
              value: photoApiPort
            }
            {
              name: 'STORAGE_ACCOUNT_NAME'
              value: storage.outputs.name
            }
            {
              name: 'STORAGE_ACCOUNT_SUFFIX'
              value: 'blob.${environment().suffixes.storage}'
            }
            {
              name: 'AZURE_CLIENT_ID'
              value: umid.properties.clientId
            }
            {
              name: 'AZURE_TENANT_ID'
              value: tenant().tenantId
            }
            {
              name: 'CORS_ORIGINS'
              value: corsOrigins
            }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 1
      }
    }
  }
}

module daprComponentUploadsStorageQueue 'modules/daprComponent.bicep' = {
  name: 'daprComponentUploadsStorageQueueDeployment'
  params: {
    containerAppEnvName: containerAppEnvironment.outputs.name
    name: 'queue-${toLower(uploadsStorageQueueName)}'
    type: 'bindings.azure.storagequeues'
    metadata: [
      {
        name: 'storageAccount'
        value: storage.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storageKey
      }
      {
        name: 'queue'
        value: uploadsStorageQueueName
      }
    ]
  }
  dependsOn: [
    resizeApi
  ]
}

module daprComponentImagesStorageQueue 'modules/daprComponent.bicep' = {
  name: 'daprComponentImagesStorageQueueDeployment'
  params: {
    containerAppEnvName: containerAppEnvironment.outputs.name
    name: 'queue-${toLower(imagesStorageQueueName)}'
    type: 'bindings.azure.storagequeues'
    metadata: [
      {
        name: 'storageAccount'
        value: storage.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storageKey
      }
      {
        name: 'queue'
        value: imagesStorageQueueName
      }
    ]
  }
  dependsOn: [
    resizeApi
  ]
}

module daprComponentImagesStorageBlob 'modules/daprComponent.bicep' = {
  name: 'daprComponentImagesStorageBlobDeployment'
  params: {
    containerAppEnvName: containerAppEnvironment.outputs.name
    name: 'blob-${toLower(imagesContainerName)}'
    type: 'bindings.azure.blobstorage'
    metadata: [
      {
        name: 'storageAccount'
        value: storage.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storageKey
      }
      {
        name: 'container'
        value: imagesContainerName
      }
    ]
  }
  dependsOn: [
    resizeApi
  ]
}

module daprComponentUploadsStorageBlob 'modules/daprComponent.bicep' = {
  name: 'daprComponentUploadsStorageBlobDeployment'
  params: {
    containerAppEnvName: containerAppEnvironment.outputs.name
    name: 'blob-${toLower(uploadsContainerName)}'
    type: 'bindings.azure.blobstorage'
    metadata: [
      {
        name: 'storageAccount'
        value: storage.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storageKey
      }
      {
        name: 'container'
        value: uploadsContainerName
      }
    ]
  }
  dependsOn: [
    resizeApi
  ]
}

resource enableCustomDomainAndCloudConnector 'Microsoft.Resources/deploymentScripts@2020-10-01' = {
  name: 'enableCustomDomain'
  location: resourceGroup().location
  kind:  'AzurePowerShell'
  properties: {
    forceUpdateTag: utcValue
    azPowerShellVersion: '7.0'
    timeout: 'PT5M'
    retentionInterval: 'PT1H'
    storageAccountSettings: {
      storageAccountName: storageAccountName
      storageAccountKey: storage.outputs.key
    }
    primaryScriptUri: 'https://github.com/cbellee/photo-api/blob/e2afcd119e7ead8171a3c1d7538656145af7f67f/cloudflare.ps1'
    arguments: '-cloudFlareApiToken ${cloudFlareApiToken} -storageAccountWebEndpoint ${storage.outputs.webEndpoint} -cloudFlareZoneId ${cloudFlareZoneId} -cName ${cNameRecord}'
  }
}

module storageCustomDomain './modules/stor.bicep' = {
  name: 'StorageCustomDomainDeployment'
  params: {
    kind: 'StorageV2'
    location: resourceGroup().location
    name: storageAccountName
    tags: tags
    containers: containers
    sku: 'Standard_LRS'
    customDomainName: cName
    deployCustomDomain: true
  }
}

output storageAccountName string = storage.outputs.name
output photoApiEndpoint string = photoApi.properties.configuration.ingress.fqdn
output resizeApiEndpoint string = resizeApi.properties.configuration.ingress.fqdn
//output cdnEndpointName string = cdnModule.outputs.cdnEndpointName
//output cdnProfileName string = cdnModule.outputs.cdnProfileName
