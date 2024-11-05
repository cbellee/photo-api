param principalId string
param roleDefinitionID string
param storageAccountName string

resource storageAccount 'Microsoft.Storage/storageAccounts@2023-01-01' existing = {
  name: storageAccountName
}

resource setRBAC 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(principalId, roleDefinitionID, resourceGroup().id)
  scope: storageAccount
  properties: {
    principalId: principalId
    roleDefinitionId: resourceId('Microsoft.Authorization/roleDefinitions', roleDefinitionID)
    principalType: 'ServicePrincipal'
  }
}
