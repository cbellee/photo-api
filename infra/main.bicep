param photoApiContainerImage string
param resizeApiContainerImage string
param faceApiContainerImage string = ''
param photoCpuResource string = '0.5'
param photoMemoryResource string = '1.0Gi'
param resizeCpuResource string = '0.25'
param resizeMemoryResource string = '0.5Gi'
param coolDownPeriod int = 600 // 10 minutes
param zoneName string = 'bellee.net'
param cNameRecord string = 'photo'
param ghcrName string = 'ghcr.io'
param githubUsername string = 'cbellee'
param jwksUrl string = 'https://0cd02bb5-3c24-4f77-8b19-99223d65aa67.ciamlogin.com/0cd02bb5-3c24-4f77-8b19-99223d65aa67/discovery/v2.0/keys?appid=689078c3-c0ad-4c10-a0d3-1c430c2e471d'
/* param utcValue string = utcNow()
param cloudFlareZoneId string
param cloudFlareApiToken string
param dnsScriptUri string
param cloudConnectorScriptUri string */

/* @secure()
param appSecret string
param tenantId string
param clientId string */

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
  {
    name: 'telemetry'
    publicAccess: 'None'
  }
]

param photoApiName string = 'photo'
param photoApiPort string = '80'
param resizeApiName string = 'resize'
param resizeApiPort string = '80'
param faceApiName string = 'face'
param faceApiPort string = '80'
param faceCpuResource string = '0.5'
param faceMemoryResource string = '1.0Gi'
param grpcMaxRequestSizeMb int = 50
param maxThumbHeight string = '300'
param maxThumbWidth string = '300'
param maxImageHeight string = '1200'
param maxImageWidth string = '1600'
param uploadsStorageQueueName string = 'uploads'
param imagesStorageQueueName string = 'images'
param imagesContainerName string = 'images'
param uploadsContainerName string = 'uploads'
param otelCollectorImage string = 'otel/opentelemetry-collector-contrib:latest'
param otelCpuResource string = '0.25'
param otelMemoryResource string = '0.5Gi'

@secure()
@description('Raw OTel collector YAML config for Azure Container Apps (otel-collector-config-aca.yml)')
param otelCollectorConfig string

var storageBlobDataOwnerRoleDefinitionID = 'b7e6dc6d-f1e8-4753-8033-0f276bb0955b'
var storageTableDataContributorRoleDefinitionID = '0a9a7e1f-b9d0-4cc4-a60d-0319b160aaa3'
var storageKey = storage.outputs.key
var storageQueueCxnString = 'DefaultEndpointsProtocol=https;AccountName=${storage.outputs.name};EndpointSuffix=${environment().suffixes.storage};AccountKey=${storageKey}'
var storageCxnString = 'DefaultEndpointsProtocol=https;AccountName=${storage.outputs.name};EndpointSuffix=${environment().suffixes.storage};AccountKey=${storageKey}'
var affix = uniqueString(resourceGroup().id)
var umidName = 'umid-${affix}'
var workspaceName = 'wks-${affix}'
var storageAccountName = 'stor${affix}'
var topicName = 'egt-${affix}'
var containerAppEnvName = 'appenv-${affix}'
var cName = '${cNameRecord}.${zoneName}'
var cNameDev = '${cNameRecord}-dev.${zoneName}'
var corsOrigins = 'http://localhost:5173,https://${cName},https://${cNameDev}'

targetScope = 'resourceGroup'

resource umid 'Microsoft.ManagedIdentity/userAssignedIdentities@2025-01-31-preview' = {
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
    setCustomDomain: false
    customDomainName: cName
    corsAllowedOrigins: [
      'http://localhost:5173'
      'https://${cName}'
      'https://${cNameDev}'
    ]
  }
}

module workspace 'br/public:avm/res/operational-insights/workspace:0.3.4' = {
  name: 'WorkspaceDeployment'
  params: {
    name: workspaceName
    tags: tags
  }
}

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

module containerAppEnvironment 'br/public:avm/res/app/managed-environment:0.13.1' = {
  name: 'ContainerAppEnvironmentDeployment'
  params: {
    name: containerAppEnvName
    appLogsConfiguration: {
      destination: 'log-analytics'
      logAnalyticsWorkspaceResourceId: workspace.outputs.resourceId
    }
    internal: false
    tags: tags
    zoneRedundant: false
    publicNetworkAccess: 'Enabled'
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

resource tableRbac 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(umid.name, 'storageTableDataContributor', affix)
  properties: {
    principalId: umid.properties.principalId
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', storageTableDataContributorRoleDefinitionID)
    principalType: 'ServicePrincipal'
  }
}

resource resizeApi 'Microsoft.App/containerApps@2025-10-02-preview' = {
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
        {
          name: 'storage-cxn'
          value: storageCxnString
        }
        {
          name: 'otel-collector-config'
          value: otelCollectorConfig
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
        transport: 'auto'
      }
    }
    managedEnvironmentId: containerAppEnvironment.outputs.resourceId
    template: {
      containers: [
        {
          image: resizeApiContainerImage
          name: resizeApiName
          probes: [
            {
              type: 'Liveness'
              timeoutSeconds: 5
              failureThreshold: 3
              initialDelaySeconds: 0
              periodSeconds: 10
              successThreshold: 1
              httpGet: {
                port: 8081
                path: '/healthz'
                scheme: 'HTTP'
              }
            }
            {
              type: 'Readiness'
              timeoutSeconds: 5
              failureThreshold: 48
              initialDelaySeconds: 0
              periodSeconds: 5
              successThreshold: 1
              httpGet: {
                port: 8081
                path: '/readyz'
                scheme: 'HTTP'
              }
            }
          ]
          resources: {
            cpu: resizeCpuResource
            memory: resizeMemoryResource
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
            {
              name: 'OTEL_EXPORTER_OTLP_ENDPOINT'
              value: 'http://localhost:4317'
            }
          ]
        }
        {
          image: otelCollectorImage
          name: 'otel-collector'
          resources: {
            cpu: otelCpuResource
            memory: otelMemoryResource
          }
          env: [
            {
              name: 'AZURE_STORAGE_CONNECTION_STRING'
              secretRef: 'storage-cxn'
            }
          ]
          args: [
            '--config=/etc/otel/config.yaml'
          ]
          volumeMounts: [
            {
              volumeName: 'otel-config'
              mountPath: '/etc/otel'
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'otel-config'
          storageType: 'Secret'
          secrets: [
            {
              secretRef: 'otel-collector-config'
              path: 'config.yaml'
            }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 2
        cooldownPeriod: coolDownPeriod
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

resource photoApi 'Microsoft.App/containerApps@2025-10-02-preview' = {
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
        {
          name: 'storage-cxn'
          value: storageCxnString
        }
        {
          name: 'otel-collector-config'
          value: otelCollectorConfig
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
            'https://${cName}'
            'https://${cNameDev}'
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
        transport: 'auto'
      }
    }
    managedEnvironmentId: containerAppEnvironment.outputs.resourceId
    template: {
      containers: [
        {
          image: photoApiContainerImage
          name: photoApiName
          resources: {
            cpu: photoCpuResource
            memory: photoMemoryResource
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
            {
              name: 'JWKS_URL'
              value: jwksUrl
            }
            {
              name: 'ROLE_NAME'
              value: 'photo.upload'
            }
            {
              name: 'OTEL_EXPORTER_OTLP_ENDPOINT'
              value: 'http://localhost:4317'
            }
            {
              name: 'FACE_STORE_TYPE'
              value: !empty(faceApiContainerImage) ? 'table' : ''
            }
            {
              name: 'TABLE_STORE_URL'
              value: storage.outputs.tableEndpoint
            }
          ]
        }
        {
          image: otelCollectorImage
          name: 'otel-collector'
          resources: {
            cpu: otelCpuResource
            memory: otelMemoryResource
          }
          env: [
            {
              name: 'AZURE_STORAGE_CONNECTION_STRING'
              secretRef: 'storage-cxn'
            }
          ]
          args: [
            '--config=/etc/otel/config.yaml'
          ]
          volumeMounts: [
            {
              volumeName: 'otel-config'
              mountPath: '/etc/otel'
            }
          ]
        }
      ]
      volumes: [
        {
          name: 'otel-config'
          storageType: 'Secret'
          secrets: [
            {
              secretRef: 'otel-collector-config'
              path: 'config.yaml'
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

// ── Face detection Container App ──────────────────────────────────────
resource faceApi 'Microsoft.App/containerApps@2025-10-02-preview' = if (!empty(faceApiContainerImage)) {
  name: faceApiName
  location: resourceGroup().location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
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
        targetPort: int(faceApiPort)
        traffic: [
          {
            latestRevision: true
            weight: 100
          }
        ]
        transport: 'auto'
      }
    }
    managedEnvironmentId: containerAppEnvironment.outputs.resourceId
    template: {
      containers: [
        {
          image: faceApiContainerImage
          name: faceApiName
          resources: {
            cpu: faceCpuResource
            memory: faceMemoryResource
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
            {
              name: 'IMAGES_QUEUE_BINDING'
              value: 'queue-${toLower(imagesStorageQueueName)}'
            }
            {
              name: 'FACE_STORE_TYPE'
              value: 'table'
            }
            {
              name: 'TABLE_STORE_URL'
              value: storage.outputs.tableEndpoint
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
              name: 'CASCADE_PATH'
              value: 'cascade/facefinder'
            }
            {
              name: 'PUPLOC_PATH'
              value: 'cascade/puploc'
            }
            {
              name: 'FLP_DIR'
              value: 'cascade/lps'
            }
          ]
        }
      ]
      scale: {
        minReplicas: 0
        maxReplicas: 2
        cooldownPeriod: coolDownPeriod
        rules: [
          {
            name: 'azure-queue-scaler'
            azureQueue: {
              queueLength: 5
              queueName: imagesStorageQueueName
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

// ── Face detection cron job (backfill) ───────────────────────────────
resource faceCronJob 'Microsoft.App/jobs@2025-10-02-preview' = if (!empty(faceApiContainerImage)) {
  name: '${faceApiName}-cron'
  location: resourceGroup().location
  tags: tags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${umid.id}': {}
    }
  }
  properties: {
    configuration: {
      registries: [
        {
          server: ghcrName
          username: githubUsername
          passwordSecretRef: 'ghcr-pull-token'
        }
      ]
      secrets: [
        {
          name: 'ghcr-pull-token'
          value: ghcrPullToken
        }
      ]
      triggerType: 'Schedule'
      scheduleTriggerConfig: {
        cronExpression: '0 3 * * *' // daily at 3 AM UTC
        parallelism: 1
        replicaCompletionCount: 1
      }
      replicaRetryLimit: 1
      replicaTimeout: 3600 // 1 hour max
    }
    environmentId: containerAppEnvironment.outputs.resourceId
    template: {
      containers: [
        {
          image: faceApiContainerImage
          name: '${faceApiName}-cron'
          command: [
            '/facecron'
          ]
          resources: {
            cpu: faceCpuResource
            memory: faceMemoryResource
          }
          env: [
            {
              name: 'FACE_STORE_TYPE'
              value: 'table'
            }
            {
              name: 'TABLE_STORE_URL'
              value: storage.outputs.tableEndpoint
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
              name: 'CASCADE_PATH'
              value: 'cascade/facefinder'
            }
            {
              name: 'PUPLOC_PATH'
              value: 'cascade/puploc'
            }
            {
              name: 'FLP_DIR'
              value: 'cascade/lps'
            }
            {
              name: 'IMAGES_CONTAINER'
              value: imagesContainerName
            }
          ]
        }
      ]
    }
  }
}

/* resource enableCustomDomainNotProxied 'Microsoft.Resources/deploymentScripts@2020-10-01' = {
  name: 'enableCustomDomainNotProxied'
  location: resourceGroup().location
  kind: 'AzurePowerShell'
  properties: {
    forceUpdateTag: utcValue
    azPowerShellVersion: '7.0'
    timeout: 'PT5M'
    retentionInterval: 'PT1H'
    storageAccountSettings: {
      storageAccountName: storageAccountName
      storageAccountKey: storage.outputs.key
    }
    primaryScriptUri: dnsScriptUri
    arguments: '-cloudFlareApiToken ${cloudFlareApiToken} -storageAccountWebEndpoint ${storage.outputs.webEndpoint} -cloudFlareZoneId ${cloudFlareZoneId} -cName ${cNameRecord}'
  }
} */

/* resource enableCloudConnector 'Microsoft.Resources/deploymentScripts@2020-10-01' = {
  name: 'enableCloudConnector'
  location: resourceGroup().location
  kind: 'AzurePowerShell'
  properties: {
    forceUpdateTag: utcValue
    azPowerShellVersion: '7.0'
    timeout: 'PT5M'
    retentionInterval: 'PT1H'
    storageAccountSettings: {
      storageAccountName: storageAccountName
      storageAccountKey: storage.outputs.key
    }
    primaryScriptUri: cloudConnectorScriptUri
    arguments: '-cloudFlareApiToken ${cloudFlareApiToken} -storageAccountWebEndpoint ${storage.outputs.webEndpoint} -cloudFlareZoneId ${cloudFlareZoneId} -cName ${cNameRecord} -ZoneName ${zoneName}'
  }
  dependsOn: []
} */

/* resource storageCustomDomain 'Microsoft.Resources/deploymentScripts@2020-10-01' = {
  name: 'setStorageCustomDomain'
  location: resourceGroup().location
  kind: 'AzurePowerShell'
  properties: {
    forceUpdateTag: utcValue
    azPowerShellVersion: '7.0'
    timeout: 'PT5M'
    retentionInterval: 'PT1H'
    storageAccountSettings: {
      storageAccountName: storageAccountName
      storageAccountKey: storage.outputs.key
    }
    scriptContent: '$SecurePassword = ConvertTo-SecureString -String ${appSecret} -AsPlainText -Force ; $TenantId = ${tenantId} ; $ApplicationId = ${clientId} ; $Credential = New-Object -TypeName System.Management.Automation.PSCredential -ArgumentList $ApplicationId, $SecurePassword ; Connect-AzAccount -ServicePrincipal -TenantId $TenantId -Credential $Credential ; $storageAccount = Get-AzStorageAccount -ResourceGroupName ${resourceGroup().name} -Name ${storageAccountName} ; $storageAccount.CustomDomain = New-Object Microsoft.Azure.Management.Storage.Models.CustomDomain ; $storageAccount.CustomDomain.Name = "${cNameRecord}.${zoneName}" ; $storageAccount.CustomDomain.UseSubDomainName = $false ; Set-AzStorageAccount -CustomDomain $storageAccount.CustomDomain -ResourceGroupName ${resourceGroup().name} -Name ${storageAccountName}'
  }
  dependsOn: [
    //enableCustomDomainNotProxied
  ]
} */

/* module storageCustomDomain './modules/stor.bicep' = {
  name: 'StorageCustomDomainDeployment'
  params: {
    kind: 'StorageV2'
    location: resourceGroup().location
    name: storageAccountName
    tags: tags
    containers: containers
    sku: 'Standard_LRS'
    customDomainName: cName
    setCustomDomain: true
  }
  dependsOn: [
    //enableCustomDomainNotProxied
    enableCloudConnector
  ]
} */

/* resource enableCustomDomainProxied 'Microsoft.Resources/deploymentScripts@2020-10-01' = {
  name: 'enableCustomDomainProxied'
  location: resourceGroup().location
  kind: 'AzurePowerShell'
  properties: {
    forceUpdateTag: utcValue
    azPowerShellVersion: '7.0'
    timeout: 'PT5M'
    retentionInterval: 'PT1H'
    storageAccountSettings: {
      storageAccountName: storageAccountName
      storageAccountKey: storage.outputs.key
    }
    primaryScriptUri: dnsScriptUri
    arguments: '-cloudFlareApiToken ${cloudFlareApiToken} -storageAccountWebEndpoint ${storage.outputs.webEndpoint} -cloudFlareZoneId ${cloudFlareZoneId} -cName ${cNameRecord} -ZoneName ${zoneName} -ProxyDns'
  }
  dependsOn: [
    storageCustomDomain
  ]
} */

output storageAccountName string = storage.outputs.name
output photoApiEndpoint string = photoApi.properties.configuration.ingress.fqdn
output resizeApiEndpoint string = resizeApi.properties.configuration.ingress.fqdn
