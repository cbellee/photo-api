param location string
param repoUrl string
param domainName string
param containerAppName string = 'photo'
param name string = 'photo-spa'

resource containerApp 'Microsoft.App/containerApps@2023-11-02-preview' existing = {
  name: containerAppName
}

resource spa 'Microsoft.Web/staticSites@2023-01-01' = {
  name: name
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

resource spa_default 'Microsoft.Web/staticSites/basicAuth@2023-01-01' = {
  parent: spa
  name: 'default'
  properties: {
    applicableEnvironmentsMode: 'SpecifiedEnvironments'
  }
}

resource spa_domainName 'Microsoft.Web/staticSites/customDomains@2023-01-01' = {
  parent: spa
  name: domainName
  properties: {}
}

resource spa_backend 'Microsoft.Web/staticSites/linkedBackends@2023-01-01' = {
  parent: spa
  name: 'api-backend'
  properties: {
    backendResourceId: containerApp.id
  }
}

output url string = spa.properties.customDomains[0]
