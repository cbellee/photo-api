param location string
param repoUrl string
param domainName string
param containerAppName string
param name string = 'spa'

var suffix = uniqueString(resourceGroup().id)
var spaName = '${name}-${suffix}'

resource containerApp 'Microsoft.App/containerApps@2024-08-02-preview' existing = {
  name: containerAppName
}

resource spa 'Microsoft.Web/staticSites@2024-04-01' = {
  name: spaName
  location: location
  sku: {
    name: 'Standard'
    tier: 'Standard'
  }
  properties: {
    repositoryUrl: repoUrl
    branch: 'main'
    stagingEnvironmentPolicy: 'Enabled'
    allowConfigFileUpdates: true
    provider: 'GitHub'
    enterpriseGradeCdnStatus: 'Disabled'
  }
}

resource spa_default 'Microsoft.Web/staticSites/basicAuth@2024-04-01' = {
  parent: spa
  name: 'default'
  properties: {
    applicableEnvironmentsMode: 'SpecifiedEnvironments'
  }
}

resource spa_backend 'Microsoft.Web/staticSites/linkedBackends@2024-04-01' = {
  parent: spa
  name: 'photo-api-backend'
  properties: {
    backendResourceId: containerApp.id
  }
}

output url string = spa.properties.customDomains[0]
