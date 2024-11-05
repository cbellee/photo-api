param domainName string
param subdomainName string
param containerAppFqdn string

resource zone 'Microsoft.Network/dnsZones@2023-07-01-preview' existing = {
  name: domainName
}

resource cname 'Microsoft.Network/dnsZones/CNAME@2023-07-01-preview' = {
  parent: zone
  name: subdomainName
  properties: {
    TTL: 3600
    CNAMERecord: {
      cname: containerAppFqdn
    }
  }
}
