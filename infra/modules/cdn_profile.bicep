param appName string = 'photo-app'
param storageAccountWebEndpoint string
param domainName string = 'gallery.bellee.net'

resource profile 'Microsoft.Cdn/profiles@2024-06-01-preview' = {
  name: '${appName}-profile'
  location: 'Global'
  sku: {
    name: 'Standard_Verizon'
  }
  properties: {}
}

resource endpoint 'Microsoft.Cdn/profiles/endpoints@2024-06-01-preview' = {
  parent: profile
  name: '${appName}-endpoint'
  location: 'Global'
  properties: {
    originHostHeader: storageAccountWebEndpoint
    contentTypesToCompress: [
      'text/plain'
      'text/html'
      'text/css'
      'text/javascript'
      'application/x-javascript'
      'application/javascript'
      'application/json'
      'application/xml'
    ]
    isCompressionEnabled: true
    isHttpAllowed: false
    isHttpsAllowed: true
    queryStringCachingBehavior: 'IgnoreQueryString'
    origins: [
      {
        name: 'default-origin'
        properties: {
          hostName: storageAccountWebEndpoint
          enabled: true
        }
      }
    ]
    originGroups: []
    geoFilters: []
    deliveryPolicy: {
      rules: [
        {
          order: 0
          conditions: []
          actions: [
            {
              name: 'CacheExpiration'
              parameters: {
                typeName: 'DeliveryRuleCacheExpirationActionParameters'
                cacheBehavior: 'SetIfMissing'
                cacheType: 'All'
                cacheDuration: '1.00:00:00'
              }
            }
          ]
        }
      ]
    }
  }
}

resource customDomain 'Microsoft.Cdn/profiles/endpoints/customdomains@2024-06-01-preview' = {
  parent: endpoint
  name: domainName
  properties: {
    hostName: domainName
  }
}

resource endpointDefaultOrigin 'Microsoft.Cdn/profiles/endpoints/origins@2024-06-01-preview' = {
  parent: endpoint
  name: 'default-origin'
  properties: {
    hostName: storageAccountWebEndpoint
    enabled: true
  }
}
