param (
  [string]
  $cloudFlareApiToken,
  [string]
  $cloudFlareZoneId,
  [string]
  $storageAccountWebEndpoint,
  [string]
  $cName
)

# Set common header
$headers = @{"Authorization" = "Bearer $cloudFlareApiToken"; "Content-Type" = "application/json" }

# Add CNAME DNS Record
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/dns_records"

$params = @{
  Uri     = $uri
  Headers = $headers
  Method  = 'POST'
  Body    = 
  @"
      {
          "comment": "CNAME record",
          "content": "$storageAccountWebEndpoint",
          "name": "$cName",
          "proxied": true,
          "ttl": 3600,
          "type": "CNAME"
      }
"@
}

try {
  $resp = Invoke-WebRequest @params -ErrorAction Stop
  if ($resp.StatusCode -ne 200) {
    throw "Failed to add DNS Record. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
  }
  else {
    Write-Output "DNS Record added successfully"
  }
}
catch {
  Write-Error "Failed to add DNS Record. $($_.Exception.Message)"
}

# Get existing Cloud Connector Rules
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/cloud_connector/rules"
$rules = @()

$params = @{
  Uri     = $uri
  Headers = $headers
  Method  = 'GET'
}

$resp = Invoke-WebRequest @params
if ($resp.StatusCode -ne 200) {
  throw "Failed to get Cloud Connector rules. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
} else {
  Write-Output "Cloud Connector rules fetched successfully"
  $rules += ($resp.Content | ConvertFrom-Json).result
}

# Add Cloud Connector Rule
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/cloud_connector/rules"

$newRule = [PSCustomObject]@{
  enabled    = $true
  expression = "(http.request.full_uri wildcard `"`")"
  provider   = "azure_storage"
  description = "Connect to Azure storage container"
  parameters = @{
    host = $storageAccountWebEndpoint
  }
}

# Ensure rules are unique with regard tyo the 'parameters' property
$uniqueRules = @()
foreach ($rule in $rules) {
  Compare-Object -ReferenceObject $rule -DifferenceObject $newRule -Property parameters -PassThru | ForEach-Object {
    if ($_.SideIndicator -eq '=>') {
      Write-Output "Cloud Connector rule already exists"
      $uniqueRules += $newRule
    } else {
      Write-Output "Cloud Connector rule does not exist, adding to rules list"
      $uniqueRules += $rule
    }
  }
}

$uniqueRules

$params = @{
  Uri     = $uri
  Headers = $headers
  Method  = 'PUT'
  Body    = $uniqueRules | ConvertTo-Json
  <# @"
    [
      {
          "enabled": true,
          "expression": "(http.request.full_uri wildcard \u0022\u0022)",
          "provider": "azure_storage",
          "description": "Connect to Azure storage container",
          "parameters": {"host": "$storageAccountWebEndpoint"}
      }
    ]
"@ #>
}

$resp = Invoke-WebRequest @params
if ($resp.StatusCode -ne 200) {
  throw "Failed to add Cloud Connector rule. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
}
else {
  Write-Output "Cloud Connector rule added successfully"
}