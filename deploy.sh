#!/bin/bash

while getopts "ls" option; do
   case $option in
      l) localDockerBuild=1
	  ;; # use '-l' cmdline flag to build containers locally
	  s) skipContainerBuild=1
	  ;; # use '-s' cmdline flag to skip container build
	  \?) echo "Invalid option: $OPTARG"
   esac
done

LOCATION='australiaeast'
SUBSCRIPTION_ID='655845bb-6fb9-4adf-bf80-5776ea887bc5'
RG_NAME="photo-app-rg"
SEMVER='0.1.0'
REV=$(git rev-parse --short HEAD)
TAG="$SEMVER-$REV"
PHOTO_API_NAME='photo'
RESIZE_API_NAME='resize'
RESIZE_API_IMAGE="$RESIZE_API_NAME:$TAG"
PHOTO_API_IMAGE="$PHOTO_API_NAME:$TAG"
AZURE_TENANT_ID='dd3cd82b-4ff1-4e7e-9220-1a9879b8d596'
STORAGE_ACCOUNT_SUFFIX='blob.core.windows.net'
UPLOADS_CONTAINER_NAME='uploads'
IMAGES_CONTAINER_NAME='images'

# az ad sp create-for-rbac --name photo-api-sp --role contributor --scopes /subscriptions/$SUBSCRIPTION_ID --json-auth

az group create --location $LOCATION --name $RG_NAME

# create ACR
echo "creating ACR"
export ACR_NAME=$(az deployment group create \
	--resource-group $RG_NAME \
	--name 'acr-deployment' \
	--template-file ./infra/modules/acr.bicep \
	--query 'properties.outputs.acrName.value' \
	--output tsv
)

if [[ $localDockerBuild != 1 && $skipContainerBuild != 1 ]]; then

	# photo API
	echo "Building image in ACR - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
		az acr build -r $ACR_NAME -t "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
		--build-arg SERVICE_NAME=$PHOTO_API_NAME \
		--build-arg SERVICE_PORT=$PHOTO_API_PORT \
		-f ./Dockerfile . &

	# resize API
	echo "Building image in ACR - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	az acr build -r $ACR_NAME -t "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE" \
		--build-arg SERVICE_NAME=$RESIZE_API_NAME \
		--build-arg SERVICE_PORT=$RESIZE_API_PORT \
		-f ./Dockerfile . &

	# face API
	# echo "Building image - TAG: '$ACR_NAME.azurecr.io/$FACE_API_IMAGE'"
	# docker build -t "$ACR_NAME.azurecr.io/$FACE_API_IMAGE" \
	# --build-arg SERVICE_NAME=$FACE_API_NAME \
	# --build-arg SERVICE_PORT=$FACE_API_PORT \
	# -f ./Dockerfile .

    # echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$FACE_API_IMAGE'"
	# docker push "$ACR_NAME.azurecr.io/$FACE_API_IMAGE"

	# wait for the background jobs to finish
	wait < <(jobs -p)

elif [[ $localDockerBuild == 1 && $skipContainerBuild != 1 ]]; then

	# login to ACR
	az acr login -n $ACR_NAME

	echo "Building image locally - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
	docker build -t "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
		--build-arg SERVICE_NAME=$PHOTO_API_NAME \
		--build-arg SERVICE_PORT=$PHOTO_API_PORT \
		-f ./Dockerfile .
	echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
	docker push "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE"

	echo "Building image locally - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	docker build -t "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE" \
		--build-arg SERVICE_NAME=$RESIZE_API_NAME \
		--build-arg SERVICE_PORT=$RESIZE_API_PORT \
		-f ./Dockerfile .
	echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	docker push "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE"

	# docker build -t "$ACR_NAME.azurecr.io/$FACE_API_IMAGE" \
	#	--build-arg SERVICE_NAME=$FACE_API_NAME \
	#	--build-arg SERVICE_PORT=$FACE_API_PORT \
	#	-f ./Dockerfile-face-detection .
	# echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$FACE_API_IMAGE'"
	# docker push "$ACR_NAME.azurecr.io/$FACE_API_IMAGE"

	# wait for the background jobs to finish
	wait < <(jobs -p)
	
else
	echo "skipping build..."
fi

az deployment group create \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--template-file ./infra/main.bicep \
	--parameters acrName=$ACR_NAME \
	--parameters photoApiContainerImage="$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
	--parameters resizeApiContainerImage="$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE"

export STORAGE_ACCOUNT_NAME="$(az deployment group show \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--query properties.outputs.storageAccountName.value \
	-o tsv)"

export PHOTO_APP_ENDPOINT_URI="$(az deployment group show \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--query properties.outputs.photoAppEndpoint.value \
	-o tsv)"

export RESIZE_APP_ENDPOINT_URI="$(az deployment group show \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--query properties.outputs.resizeAppEndpoint.value \
	-o tsv)"

# enable static website hosting
az storage blob service-properties update --account-name $STORAGE_ACCOUNT_NAME --static-website --index-document index.html --404-document index.html

cd ../photo-spa

# replace tokens in apiConfig_template.js with actual values
sed "s/{{AZURE_TENANT_ID}}/$AZURE_TENANT_ID/g ; \
    s/{{STORAGE_ACCOUNT_NAME}}/$STORAGE_ACCOUNT_NAME/g ; \
    s/{{STORAGE_ACCOUNT_SUFFIX}}/$STORAGE_ACCOUNT_SUFFIX/g ; \
    s/{{PHOTO_APP_ENDPOINT_URI}}/$PHOTO_APP_ENDPOINT_URI/g" \
    ./src/config/apiConfig_template.js > ./src/config/apiConfig.js

# build javascript
npm run build

# enable azure storage static website
az storage blob service-properties update --account-name $STORAGE_ACCOUNT_NAME --static-website --index-document index.html --404-document index.html

# upload to Azure Blob Storage
az storage azcopy blob upload --container '$web' --account-name $STORAGE_ACCOUNT_NAME --source './dist/*' --recursive

# purge the CDN cache 
az cdn endpoint purge --resource-group $RG_NAME --name photo-app-endpoint --profile-name photo-app-profile --content-paths "/*"

cd ../photo-api
