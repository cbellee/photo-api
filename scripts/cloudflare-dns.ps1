param (
  [string]
  $cloudFlareApiToken,
  [string]
  $cloudFlareZoneId,
  [string]
  $storageAccountWebEndpoint,
  [string]
  $cName,
  [switch]
  $ProxyDns
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
          "proxied": $($ProxyDns.IsPresent.ToString().ToLower()),
          "ttl": 3600,
          "type": "CNAME"
      }
"@
}

Write-Output "Params: $($params)"
Write-Output "Body: $($params.Body)"

$resp = Invoke-WebRequest @params -SkipHttpErrorCheck
if ($resp.StatusCode -ne 200) {
  Write-Output "Failed to add DNS Record. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
}
else {
  Write-Output "DNS Record added successfully"
}
