param (
  [string]
  $cloudFlareApiToken,
  [string]
  $cloudFlareZoneId,
  [string]
  $storageAccountWebEndpoint,
  [string]
  $cName,
  [boolean]
  $isDnsProxied = $false
)

$ErrorActionPreference = 'Continue'

# Set common header
$headers = @{"Authorization" = "Bearer $cloudFlareApiToken"; "Content-Type" = "application/json" }

# Get existing Cloud Connector Rules
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/cloud_connector/rules"
$rules = @()

$params = @{
  Uri     = $uri
  Headers = $headers
  Method  = 'GET'
}

$resp = Invoke-WebRequest @params -SkipHttpErrorCheck
if ($resp.StatusCode -ne 200) {
  throw "Failed to get Cloud Connector rules. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
}
else {
  Write-Output "Cloud Connector rules fetched successfully"
  $rules += ($resp.Content | ConvertFrom-Json -Depth 10).result
}

# Add Cloud Connector Rule
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/cloud_connector/rules"

$newRule = [PSCustomObject]@{
  enabled     = $true
  expression  = "(http.request.full_uri wildcard `"`")"
  provider    = "azure_storage"
  description = "Connect to Azure storage endpoint: $storageAccountWebEndpoint"
  parameters  = @{
    host = $storageAccountWebEndpoint
  }
}

# Ensure rules are unique with regard tyo the 'parameters' property
if ($rules.parameters.host -notcontains $newRule.parameters.host) {
  $rules += $newRule
}

$params = @{
  Uri     = $uri
  Headers = $headers
  Method  = 'PUT'
  Body    = $rules | ConvertTo-Json -Depth 10
}

$resp = Invoke-WebRequest @params -SkipHttpErrorCheck
if ($resp.StatusCode -ne 200) {
  Write-Output "Failed to add Cloud Connector rule. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
}
else {
  Write-Output "Cloud Connector rule added successfully"
}