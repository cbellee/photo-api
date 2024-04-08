param location string
param acrName string
param tag string
param staticWebAppLocation string = 'eastasia'
param domainName string
param subDomainName string
param repoUrl string = 'https://github.com/cbellee/photo-spa'
param dnsZoneResourceGroupName string = 'external-dns-zones-rg'

param resizeApiName string
param resizeApiPort string
param photoApiName string
param photoApiPort string
// param faceApiName string
// param faceApiPort string

param uploadsStorageQueueName string
param imagesStorageQueueName string
param thumbsContainerName string
param imagesContainerName string
param uploadsContainerName string

param maxThumbHeight string
param maxThumbWidth string
param maxImageHeight string
param maxImageWidth string

param grpcMaxRequestSizeMb int

param tags object = {
  environment: 'dev'
  costcode: '1234567890'
}

var affix = uniqueString(resourceGroup().id)
var containerAppEnvName = 'app-env-${affix}'

var resizeApiContainerImage = '${acr.properties.loginServer}/${resizeApiName}:${tag}'
var photoApiContainerImage = '${acr.properties.loginServer}/${photoApiName}:${tag}'
// var faceApiContainerImage = '${acr.properties.loginServer}/${faceApiName}:${tag}'

var storageBlobDataOwnerRoleDefinitionID = 'b7e6dc6d-f1e8-4753-8033-0f276bb0955b'
var acrPullRoleDefinitionId = '7f951dda-4ed3-4680-a7ca-43fe172d538d'
var storageQueueCxnString = 'DefaultEndpointsProtocol=https;AccountName=${storModule.outputs.name};EndpointSuffix=${environment().suffixes.storage};AccountKey=${storModule.outputs.key}'
var acrLoginServer = '${acrName}.azurecr.io'
var workspaceName = 'wks-${affix}'
var storageAccountName = 'stor${affix}'
var aiName = 'ai-${affix}'
var acrPullUmidName = 'acrpull-umid-${affix}'

resource umid 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  location: location
  name: acrPullUmidName
  tags: tags
}

module acrPullRole 'modules/acrpull-rbac.bicep' = {
  name: 'module-acrpull-rbac'
  params: {
    acrName: acrName
    principalId: umid.properties.principalId
    roleDefinitionID: acrPullRoleDefinitionId
  }
}

module aiModule 'modules/ai.bicep' = {
  name: 'module-ai'
  params: {
    location: location
    aiName: aiName
  }
}

resource acr 'Microsoft.ContainerRegistry/registries@2021-12-01-preview' existing = {
  name: acrName
}

module wksModule 'modules/wks.bicep' = {
  name: 'module-wks'
  params: {
    location: location
    name: workspaceName
    tags: tags
  }
}

module storModule 'modules/stor.bicep' = {
  name: 'module-stor'
  params: {
    location: location
    name: storageAccountName
    containers: [
      {
        name: thumbsContainerName
        publicAccess: 'Blob'
      }
      {
        name: imagesContainerName
        publicAccess: 'Blob'
      }
      {
        name: uploadsContainerName
        publicAccess: 'None'
      }
    ]
    tags: tags
  }
}

module eventGridModule 'modules/eventgrid.bicep' = {
  name: 'module-evg'
  params: {
    name: 'egt'
    containers: [
      uploadsContainerName
    ]
    storageAccountId: storModule.outputs.id
    location: location
    eventTypes: [
      'Microsoft.Storage.BlobCreated'
    ]
    topicSourceId: storModule.outputs.id
    tags: tags
  }
}

module containerAppEnvModule './modules/cappenv.bicep' = {
  name: 'module-capp-env'
  params: {
    name: containerAppEnvName
    location: location
    isInternal: false
    tags: tags
    wksSharedKey: wksModule.outputs.workspaceSharedKey
    aiKey: aiModule.outputs.aiKey
    wksCustomerId: wksModule.outputs.workspaceCustomerId
  }
}

resource resizeApi 'Microsoft.App/containerApps@2023-04-01-preview' = {
  name: resizeApiName
  location: location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
  dependsOn: [
    containerAppEnvModule
    storModule
    eventGridModule
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
      ]
      registries: [
        {
          server: acrLoginServer
          identity: umid.id
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
    managedEnvironmentId: containerAppEnvModule.outputs.id
    template: {
      containers: [
        {
          image: resizeApiContainerImage
          name: resizeApiName
          resources: {
            cpu: '0.25'
            memory: '0.5Gi'
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
              name: 'THUMBS_CONTAINER_BINDING'
              value: 'blob-${thumbsContainerName}'
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
              value: storModule.outputs.name
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
        minReplicas: 1
        maxReplicas: 10
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

resource photoApi 'Microsoft.App/containerApps@2023-04-01-preview' = {
  name: photoApiName
  location: location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
  dependsOn: [
    containerAppEnvModule
    storModule
    eventGridModule
  ]
  properties: {
    configuration: {
      activeRevisionsMode: 'single'
      registries: [
        {
          server: acrLoginServer
          identity: umid.id
        }
      ]
      ingress: {
        corsPolicy: {
          allowedOrigins: [
            'http://localhost:3000'
            'http://127.0.0.1:5173'
          ]
          allowedHeaders: [
            'Access-Control-Allow-Origin'
          ]
          allowedMethods: [
            'GET'
            'OPTIONS'
            'HEAD'
          ]
          exposeHeaders: [
            'Access-Control-Allow-Origin'
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
    managedEnvironmentId: containerAppEnvModule.outputs.id
    template: {
      containers: [
        {
          image: photoApiContainerImage
          name: photoApiName
          resources: {
            cpu: '0.25'
            memory: '0.5Gi'
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
              value: storModule.outputs.name
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
        minReplicas: 1
        maxReplicas: 10
      }
    }
  }
}

// grant resize container app System Managed Identity 'Storage Blob Data Owner' permission on the storage account
module rbacBlobPermission 'modules/rbac.bicep' = {
  name: 'module-blob-rbac'
  params: {
    principalId: umid.properties.principalId
    roleDefinitionID: storageBlobDataOwnerRoleDefinitionID
  }
}

/* resource faceApi 'Microsoft.App/containerApps@2022-10-01' = {
  name: faceApiName
  location: location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
  dependsOn: [
    containerAppEnvModule
  ]
  properties: {
    configuration: {
      activeRevisionsMode: 'single'
      dapr: {
        appId: faceApiName
        appPort: int(faceApiPort)
        appProtocol: 'grpc'
        enabled: true
        httpMaxRequestSize: grpcMaxRequestSizeMb
      }
      secrets: [
        {
          name: 'storage-queue-cxn'
          value: storageQueueCxnString
        }
      ]
      registries: [
        {
          server: acrLoginServer
          identity: umid.id
        }
      ]
      ingress: {
        external: true
        targetPort: int(faceApiPort)
        traffic: [
          {
            latestRevision: true
            weight: 100
          }
        ]
        transport: 'http'
        corsPolicy: {
          allowedOrigins: [
            '*'
          ]
          allowedMethods: [
            'GET'
            'POST'
            'OPTIONS'
            'DELETE'
            'PUT'
          ]
          allowedHeaders: [
            '*'
          ]
          allowCredentials: true
        }
      }
    }
    managedEnvironmentId: containerAppEnvModule.outputs.id
    template: {
      containers: [
        {
          image: faceApiContainerImage
          name: faceApiName
          resources: {
            cpu: '0.25'
            memory: '0.5Gi'
          }
          env: [
            {
              name: 'SERVICE_NAME'
              value: faceApiName
            }
            {
              name: 'SERVICE_PORT'
              value: faceApiPort
            }
          ]
        }
      ]
      scale: {
        minReplicas: 1
        maxReplicas: 10
      }
    }
  }
}
 */

resource uploadsStorageQueueDaprComponent 'Microsoft.App/managedEnvironments/daprComponents@2022-06-01-preview' = {
  dependsOn: [
    containerAppEnvModule
  ]
  name: '${containerAppEnvName}/queue-${toLower(uploadsStorageQueueName)}'
  properties: {
    componentType: 'bindings.azure.storagequeues'
    version: 'v1'
    ignoreErrors: false
    initTimeout: '60s'
    metadata: [
      {
        name: 'storageAccount'
        value: storModule.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storModule.outputs.key
      }
      {
        name: 'queue'
        value: uploadsStorageQueueName
      }
    ]
    scopes: [
      resizeApiName
    ]
  }
}

resource imagesStorageQueueDaprComponent 'Microsoft.App/managedEnvironments/daprComponents@2022-06-01-preview' = {
  dependsOn: [
    containerAppEnvModule
  ]
  name: '${containerAppEnvName}/queue-${toLower(imagesStorageQueueName)}'
  properties: {
    componentType: 'bindings.azure.storagequeues'
    version: 'v1'
    ignoreErrors: false
    initTimeout: '60s'
    metadata: [
      {
        name: 'storageAccount'
        value: storModule.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storModule.outputs.key
      }
      {
        name: 'queue'
        value: imagesStorageQueueName
      }
    ]
    scopes: [
      resizeApiName
    ]
  }
}

resource uploadsStorageDaprComponent 'Microsoft.App/managedEnvironments/daprComponents@2022-06-01-preview' = {
  dependsOn: [
    containerAppEnvModule
  ]
  name: '${containerAppEnvName}/blob-${toLower(uploadsContainerName)}'
  properties: {
    componentType: 'bindings.azure.blobstorage'
    version: 'v1'
    ignoreErrors: false
    initTimeout: '60s'
    metadata: [
      {
        name: 'storageAccount'
        value: storModule.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storModule.outputs.key
      }
      {
        name: 'container'
        value: uploadsContainerName
      }
    ]
    scopes: [
      resizeApiName
    ]
  }
}

resource thumbsStorageDaprComponent 'Microsoft.App/managedEnvironments/daprComponents@2022-06-01-preview' = {
  dependsOn: [
    containerAppEnvModule
  ]
  name: '${containerAppEnvName}/blob-${toLower(thumbsContainerName)}'
  properties: {
    componentType: 'bindings.azure.blobstorage'
    version: 'v1'
    ignoreErrors: false
    initTimeout: '60s'
    metadata: [
      {
        name: 'storageAccount'
        value: storModule.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storModule.outputs.key
      }
      {
        name: 'container'
        value: thumbsContainerName
      }
    ]
    scopes: [
      resizeApiName
    ]
  }
}

resource imagesStorageDaprComponent 'Microsoft.App/managedEnvironments/daprComponents@2022-06-01-preview' = {
  dependsOn: [
    containerAppEnvModule
  ]
  name: '${containerAppEnvName}/blob-${toLower(imagesContainerName)}'
  properties: {
    componentType: 'bindings.azure.blobstorage'
    version: 'v1'
    ignoreErrors: false
    initTimeout: '60s'
    metadata: [
      {
        name: 'storageAccount'
        value: storModule.outputs.name
      }
      {
        name: 'storageAccessKey'
        value: storModule.outputs.key
      }
      {
        name: 'container'
        value: imagesContainerName
      }
    ]
    scopes: [
      resizeApiName
    ]
  }
}

module cname 'modules/dns.bicep' = {
  name: 'cname-module'
  scope: resourceGroup(dnsZoneResourceGroupName)
  params: {
    containerAppFqdn: photoApi.properties.configuration.ingress.fqdn
    domainName: domainName
    subdomainName: subDomainName
  }
}

module staticWebApp 'modules/staticwebapp.bicep' = {
  name: 'module-static-web-app'
  params: {
    containerAppName: photoApiName
    domainName: '${subDomainName}.${domainName}'
    location: staticWebAppLocation
    repoUrl: repoUrl
    name: 'photo-spa'
  }
  dependsOn: [
    cname
  ]
}

output resizeUrl string = resizeApi.properties.configuration.ingress.fqdn
output storageAccount string = storModule.outputs.name
output webAPpUrl string = staticWebApp.outputs.url
