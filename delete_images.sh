STORAGE_ACCOUNT_NAME=''

az storage blob delete-batch --account-name $STORAGE_ACCOUNT_NAME --source uploads
az storage blob delete-batch --account-name $STORAGE_ACCOUNT_NAME --source images