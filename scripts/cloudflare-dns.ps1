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
          "proxied": $isDnsProxied,
          "ttl": 3600,
          "type": "CNAME"
      }
"@
}

$resp = Invoke-WebRequest @params -SkipHttpErrorCheck
if ($resp.StatusCode -ne 200) {
  Write-Output "Failed to add DNS Record. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
}
else {
  Write-Output "DNS Record added successfully"
}
