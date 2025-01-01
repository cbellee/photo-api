STORAGE_ACCOUNT_NAME='stor6aq2g56sfcosi'

az storage blob delete-batch --account-name $STORAGE_ACCOUNT_NAME --source uploads
az storage blob delete-batch --account-name $STORAGE_ACCOUNT_NAME --source images
