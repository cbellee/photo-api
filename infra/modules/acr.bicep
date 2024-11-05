param location string = resourceGroup().location

@allowed([
  'Basic'
  'Standard'
  'Premium'
])
param sku string = 'Basic'

var affix = uniqueString(resourceGroup().id)
var acrName = '${affix}acr'

resource acr 'Microsoft.ContainerRegistry/registries@2023-11-01-preview' = {
  name: acrName
  location: location
  sku: {
    name: sku
  }
  properties: {
    adminUserEnabled: true
  }
}

output acrName string = acr.name
output acrLoginServer string = acr.properties.loginServer
