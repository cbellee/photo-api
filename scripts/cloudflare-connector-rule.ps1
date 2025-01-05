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

$ErrorActionPreference = 'Continue'

# Set common header
$headers = @{"Authorization" = "Bearer $cloudFlareApiToken"; "Content-Type" = "application/json" }

# Get existing Cloud Connector Rules
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/cloud_connector/rules"

function Get-CloudConnectorRules {
  param(
    $uri,
    $headers
  )

  $params = @{
    Uri     = $uri
    Headers = $headers
    Method  = 'GET'
  }

  $rules = @()
  $resp = Invoke-WebRequest @params -SkipHttpErrorCheck
  if ($resp.StatusCode -ne 200) {
    # Write-Information "Failed to get Cloud Connector rules. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
    return $null
  }
  else {
    # Write-Information "Cloud Connector rules fetched successfully"
    $rules += ($resp.Content | ConvertFrom-Json -Depth 10)
    return $rules.result
  }
}

# Add Cloud Connector Rule
function Add-CloudConnectorRule {
  param (
    $uri,
    $headers,
    $storageAccountWebEndpoint,
    $cName
  )
  
  $rules = @()
  $newRule = [PSCustomObject]@{
    enabled     = $true
    expression  = "(http.request.full_uri wildcard `"`")"
    provider    = "azure_storage"
    description = "Connect to Azure storage endpoint: $storageAccountWebEndpoint"
    parameters  = @{
      host = $storageAccountWebEndpoint
    }
  }

  $rules += $newRule

  $params = @{
    Uri     = $uri
    Headers = $headers
    Method  = 'PUT'
    Body    = ConvertTo-Json -InputObject @( $rules ) -Depth 10 -Compress
  }

  $resp = Invoke-WebRequest @params -SkipHttpErrorCheck
  if ($resp.StatusCode -ne 200) {
    Write-Information "Failed to add Cloud Connector rule. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
  }
  else {
    Write-Information "Cloud Connector rule added successfully"
  }
}

$rules = Get-CloudConnectorRules -uri $uri -headers $headers
if ($rules.parameters.host -notcontains $storageAccountWebEndpoint) {
  Write-Output "Cloud Connector rule does not exist, adding it"
  Add-CloudConnectorRule -uri $uri -headers $headers -storageAccountWebEndpoint $storageAccountWebEndpoint -cName $cName
}
else {
  Write-Output "Cloud Connector rule already exists"
}
