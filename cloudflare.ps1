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

# Add CNAME DNS Record
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/dns_records"

$params = @{
  Uri     = $uri
  Headers = @{"Authorization" = "Bearer $cloudFlareApiToken"; "Content-Type" = "application/json" }
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

# Add Cloud Connector Rule
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/cloud_connector/rules";

$params = @{
  Uri     = $uri
  Headers = @{"Authorization" = "Bearer $cloudFlareApiToken"; "Content-Type" = "application/json" }
  Method  = 'PUT'
  Body    = 
  @"
    [
      {
          "enabled": true,
          "expression": "(http.request.full_uri wildcard \u0022\u0022)",
          "provider": "azure_storage",
          "description": "Connect to Azure storage container",
          "parameters": {"host": "$storageAccountWebEndpoint"}
      }
    ]
"@
}

$resp = Invoke-WebRequest @params
if ($resp.StatusCode -ne 200) {
  throw "Failed to add Cloud Connector rule. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
}
else {
  Write-Output "Cloud Connector rule added successfully"
}