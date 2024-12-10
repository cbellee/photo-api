STORAGE_ACCOUNT_NAME='storhw3eyjlyy236y'

az storage blob delete-batch --account-name $STORAGE_ACCOUNT_NAME --source uploads
az storage blob delete-batch --account-name $STORAGE_ACCOUNT_NAME --source images
