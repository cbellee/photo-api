param metadata array
param containerAppEnvName string
param name string
param type string
param version string = 'v1'
param timeout string = '60s'

resource uploadsStorageQueueDaprComponent 'Microsoft.App/managedEnvironments/daprComponents@2025-10-02-preview' = {
  name: '${containerAppEnvName}/${name}'
  properties: {
    componentType: type
    version: version
    ignoreErrors: false
    initTimeout: timeout
    metadata: metadata
  }
}
