CLOUDFLARE_API_TOKEN=''
CLOUDFLARE_API_KEY=''
CLOUDFLARE_EMAIL=''
CLOUDFLARE_ZONE_ID=''
STORAGE_ACCOUNT_WEB_ENDPOINT='<storageAccountName>.z8.web.core.windows.net'
CNAME='photo'
ZONE_NAME='bellee.net'

 curl --request POST "https://api.cloudflare.com/client/v4/zones/${CLOUDFLARE_ZONE_ID}/dns_records" \
        -header 'Content-Type: application/json' \
        -header "X-Auth-Email: ${CLOUDFLARE_EMAIL}" \
        -header "X-Auth-Key: ${CLOUDFLARE_API_KEY}" \
        -data \
        "
        {
          \"comment\": \"CNAME record\", \
          \"content\": \"$STORAGE_ACCOUNT_WEB_ENDPOINT\", \
          \"name\": \"$CNAME\", \
          \"proxied\": true, \
          \"ttl\": 3600, \
          \"type\": \"CNAME\"
        }
        "

curl --request PUT "https://api.cloudflare.com/client/v4/zones/${CLOUDFLARE_ZONE_ID}/cloud_connector/rules" \
        --header "Authorization: Bearer $CLOUDFLARE_API_TOKEN" \
        --header "Content-Type: application/json" \
        --data \
        "
          [
            {
                \"enabled\": true, \
                \"expression\": \"(http.request.full_uri wildcard)\", \
                \"provider\": \"azure_storage\", \
                \"description\": \"Connect to Azure storage container\", \
                \"parameters\": {\"host\": \"${STORAGE_ACCOUNT_WEB_ENDPOINT}\"}
            }
          ]
        "

curl -X GET "https://api.cloudflare.com/client/v4/user/tokens/verify" \
     -H "Authorization: Bearer ${CLOUDFLARE_API_TOKEN}" \
     -H "Content-Type:application/json"