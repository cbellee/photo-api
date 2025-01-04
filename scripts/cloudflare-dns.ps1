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
  $ProxyDns,
  [switch]
  $AddRecord,
  [switch]
  $RemoveRecord,
  [string]
  $ZoneName
)

function Get-DnsRecords {
  param (
    $uri,
    $headers
  )

  $params = @{
    Uri     = $uri
    Headers = $headers
    Method  = 'GET'
  }

  $resp = Invoke-WebRequest @params -SkipHttpErrorCheck
  if ($resp.StatusCode -ne 200) {
    throw "Failed to get DNS Records. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
  }
  else {
    Write-Output "DNS Records fetched successfully"
    return ($resp.Content | ConvertFrom-Json -Depth 10).result
  }
}

function Add-DnsRecord {
  param (
    $uri,
    $headers,
    $storageAccountWebEndpoint,
    $cName,
    $ProxyDns
  )

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
}

function Remove-DnsRecord {
  param (
    $uri,
    $headers,
    $id
  ) 
  $params = @{
    Uri     = "$uri/$id"
    Headers = $headers
    Method  = 'DELETE'
  }

  $resp = Invoke-WebRequest @params -SkipHttpErrorCheck
  if ($resp.StatusCode -ne 200) {
    Write-Output "Failed to remove DNS Record. Code: $($resp.StatusCode) Desc: $($resp.StatusDescription)"
  }
  else {
    Write-Output "DNS Record removed successfully"
  }
}

###############
# Main
###############

$ErrorActionPreference = 'Continue'

# Set common header
$headers = @{"Authorization" = "Bearer $cloudFlareApiToken"; "Content-Type" = "application/json" }

# Add CNAME DNS Record
$uri = "https://api.cloudflare.com/client/v4/zones/$cloudFlareZoneId/dns_records"

# Check if DNS Record exists, if not add the record
$recordFullName = "$cName.$ZoneName"
$dnsRecords = Get-DnsRecords -uri $uri -headers $headers
if ($dnsRecords.name -notcontains $recordFullName) {
  Add-DnsRecord -uri $uri -headers $headers -storageAccountWebEndpoint $storageAccountWebEndpoint -cName $cName -ProxyDns:$ProxyDns
}
else { # if record exists, remove and add the new record
  Write-Output "DNS Record already exists, removing & re-adding record"
  $recordToRemove = $dnsRecords | Where-Object { $_.name -eq $recordFullName }
  Remove-DnsRecord -uri $uri -headers $headers -id $recordToRemove.id
  Add-DnsRecord -uri $uri -headers $headers -storageAccountWebEndpoint $storageAccountWebEndpoint -cName $cName -ProxyDns:$ProxyDns
}
