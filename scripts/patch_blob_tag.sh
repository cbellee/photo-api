#!/bin/bash

# Script to find and update blob index tags in Azure Storage
# Usage: ./patch.sh <storage_account> <container_name> <tag_key> <old_value> <new_value>

set -e

# Check if all required parameters are provided
if [ $# -ne 5 ]; then
    echo "Usage: $0 <storage_account> <container_name> <tag_key> <old_value> <new_value>"
    echo "Example: $0 mystorageaccount mycontainer status pending processed"
    exit 1
fi

STORAGE_ACCOUNT="$1"
CONTAINER_NAME="$2"
TAG_KEY="$3"
OLD_VALUE="$4"
NEW_VALUE="$5"

echo "Finding blobs with tag '$TAG_KEY=$OLD_VALUE' in container '$CONTAINER_NAME'..."

# Find blobs with the specified tag value using blob index query
QUERY="""@container""='$CONTAINER_NAME' and ""$TAG_KEY""='$OLD_VALUE'"
echo $QUERY

BLOB_LIST=$(az storage blob filter \
    --account-name "$STORAGE_ACCOUNT" \
    --tag-filter "$QUERY" \
    --query "[].name")

echo $BLOB_LIST

    # Check if any blobs were found
    if [ -z "$BLOB_LIST" ]; then
        echo "No blobs found with tag '$TAG_KEY=$OLD_VALUE'"
        exit 0
    fi

    echo "Found blobs to update. Processing..."

    # Process each blob and update the tag
    echo "$BLOB_LIST" | jq -r '.[]' | while read -r blob; do
        echo "Updating blob: $blob"
        shortName=$(echo $blob | cut -d'/' -f3 | cut -d'.' -f1)
        echo "shortname: $shortName"

        az storage blob tag set \
            --account-name "$STORAGE_ACCOUNT" \
            --container-name "$CONTAINER_NAME" \
            --name "$blob" \
            --tags collection=travel albumImage=false album="Balkans Cruise 2025" collectionImage=false description="$shortName" isDeleted=false name="$blob"
        echo "Updated tag for blob: $blob"
    done

    echo "All blobs updated successfully!"
