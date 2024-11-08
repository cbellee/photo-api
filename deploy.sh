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
RG_NAME="photo-gallery-rg"
SEMVER='0.1.0'
REV=$(git rev-parse --short HEAD)
TAG="$SEMVER-$REV"
PHOTO_API_NAME='photo'
RESIZE_API_NAME='resize'
RESIZE_API_IMAGE="$RESIZE_API_NAME:$TAG"
PHOTO_API_IMAGE="$PHOTO_API_NAME:$TAG"
STATIC_WEBSITE_URL='https://stor465uve6pto35e.z8.web.core.windows.net'

source .env

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

	# resize API
	echo "Building image in ACR - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	az acr build -r $ACR_NAME -t "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE" \
		--build-arg SERVICE_NAME=$RESIZE_API_NAME \
		--build-arg SERVICE_PORT=$RESIZE_API_PORT \
		-f ./Dockerfile .

	# photo API
	echo "Building image in ACR - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
		az acr build -r $ACR_NAME -t "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
		--build-arg SERVICE_NAME=$PHOTO_API_NAME \
		--build-arg SERVICE_PORT=$PHOTO_API_PORT \
		-f ./Dockerfile .

	# face API
	# echo "Building image - TAG: '$ACR_NAME.azurecr.io/$FACE_API_IMAGE'"
	# docker build -t "$ACR_NAME.azurecr.io/$FACE_API_IMAGE" \
	# --build-arg SERVICE_NAME=$FACE_API_NAME \
	# --build-arg SERVICE_PORT=$FACE_API_PORT \
	# -f ./Dockerfile .

    # echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$FACE_API_IMAGE'"
	# docker push "$ACR_NAME.azurecr.io/$FACE_API_IMAGE"

elif [[ $localDockerBuild == 1 && $skipContainerBuild != 1 ]]; then

	# login to ACR
	az acr login -n $ACR_NAME
	
	echo "Building image locally - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	docker build -t "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE" \
		--build-arg SERVICE_NAME=$RESIZE_API_NAME \
		--build-arg SERVICE_PORT=$RESIZE_API_PORT \
		-f ./Dockerfile .
	echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	docker push "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE"

	echo "Building image locally - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
	docker build -t "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
		--build-arg SERVICE_NAME=$PHOTO_API_NAME \
		--build-arg SERVICE_PORT=$PHOTO_API_PORT \
		-f ./Dockerfile .
	echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
	docker push "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE"

	# docker build -t "$ACR_NAME.azurecr.io/$FACE_API_IMAGE" \
	#	--build-arg SERVICE_NAME=$FACE_API_NAME \
	#	--build-arg SERVICE_PORT=$FACE_API_PORT \
	#	-f ./Dockerfile-face-detection .
	# echo "Pushing image - TAG: '$ACR_NAME.azurecr.io/$FACE_API_IMAGE'"
	# docker push "$ACR_NAME.azurecr.io/$FACE_API_IMAGE"
else
	echo "skipping build..."
fi

az deployment group create \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--template-file ./infra/main.bicep \
	--parameters acrName=$ACR_NAME \
	--parameters photoApiContainerImage="$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
	--parameters resizeApiContainerImage="$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE" \
	--parameters staticWebSiteUrl=$STATIC_WEBSITE_URL

export STORAGE_ACCOUNT_NAME="$(az deployment group show \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--query properties.outputs.storageAccountName.value \
	-o tsv).blob.core.windows.net"
