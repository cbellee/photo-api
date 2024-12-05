param name string
param image string
param registries array
param environmentVars array
param scale object
param managementEnvironmentId string
param isExternal bool = true
param targetPort int = 80
param corsPolicy object
param tags object
param identity object
param resources object
param dapr object 
param secrets array
param traffic array

@allowed(['http', 'tcp'])
param transport string = 'http'

@allowed(['single', 'multiple'])
param revisionMode string = 'single'

resource app 'Microsoft.App/containerApps@2024-08-02-preview' = {
  name: name
  location: resourceGroup().location
  tags: tags
  identity: identity
  properties: {
    configuration: {
      secrets: secrets
      dapr: dapr
      activeRevisionsMode: revisionMode
      registries: registries
      ingress: {
        corsPolicy: corsPolicy
        external: isExternal
        targetPort: targetPort
        traffic: traffic
        transport: transport
      }
    }
    managedEnvironmentId: managementEnvironmentId
    template: {
      containers: [
        {
          image: image
          name: name
          resources: resources
          env: environmentVars
        }
      ]
      scale: scale
    }
  }
}

output appEndpoint string = app.properties.configuration.ingress.fqdn
