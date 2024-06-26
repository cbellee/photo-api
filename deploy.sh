#!/bin/bash

while getopts ":s" option; do
   case $option in
      s) skipBuild=1; # use '-s' cmdline flag to skip the container build step
   esac
done

LOCATION='australiaeast'
RG_NAME="go-photo-rg"
SEMVER='0.1.0'
REV=$(git rev-parse --short HEAD)
TAG="$SEMVER-$REV"
DOMAIN_NAME='bellee.net'
SUBDOMAIN_NAME='gallery'

DOMAIN_NAME="kainiindustries.net"
STORAGE_ACCOUNT_NAME="storqra2f23aqljtm.blob.core.windows.net"
# STORAGE_ACCOUNT_NAME="127.0.0.1:10000/devstoreaccount1"
PHOTO_APP_ID="api://a845082b-e22d-49a8-8abb-e8484609abd7"
UPLOAD_APP_ID="api://18911b98-3bf5-4a05-a417-8a12e496c9e5"
PHOTO_READ_SCOPE="${PHOTO_APP_ID}/Photo.Read"
PHOTO_WRITE_SCOPE="${PHOTO_APP_ID}/Photo.Write"
UPLOAD_READ_SCOPE="${UPLOAD_APP_ID}/Upload.Read"
UPLOAD_WRITE_SCOPE="${UPLOAD_APP_ID}/Upload.Write"

RESIZE_API_NAME="resize"
RESIZE_API_PORT="443"
PHOTO_API_NAME="photo"
PHOTO_API_PORT="443"
FACE_API_NAME="face"
FACE_API_PORT="443"

GRPC_MAX_REQUEST_SIZE_MB="30"

RESIZE_API_IMAGE="$RESIZE_API_NAME:$TAG"
STORE_API_IMAGE="$STORE_API_NAME:$TAG"
PHOTO_API_IMAGE="$PHOTO_API_NAME:$TAG"
FACE_API_IMAGE="$FACE_API_NAME:$TAG"

UPLOADS_QUEUE_NAME='uploads'
IMAGES_QUEUE_NAME='images'
IMAGES_CONTAINER_NAME='images'
UPLOADS_CONTAINER_NAME='uploads'

MAX_THUMB_HEIGHT='300'
MAX_THUMB_WIDTH='300'
MAX_IMAGE_HEIGHT='1200'
MAX_IMAGE_WIDTH='1600'

az group create --location $LOCATION --name $RG_NAME

if [[ $skipBuild != 1 ]]; then
	az deployment group create \
		--resource-group $RG_NAME \
		--name 'acr-deployment' \
		--parameters anonymousPullEnabled=true \
		--template-file ../infra/modules/acr.bicep
fi

ACR_NAME=$(az deployment group show --resource-group $RG_NAME --name 'acr-deployment' --query properties.outputs.acrName.value -o tsv)

if [[ $skipBuild != 1 ]]; then

	# build image in ACR
	az acr login -n $ACR_NAME 

	# resize API
	echo "Building image - TAG: '$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE'"
	docker build -t "$ACR_NAME.azurecr.io/$RESIZE_API_IMAGE" \
	--build-arg SERVICE_NAME=$RESIZE_API_NAME \
	--build-arg SERVICE_PORT=$RESIZE_API_PORT \
	-f ./Dockerfile .

	# photo API
	echo "Building image - TAG: '$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE'"
	docker build -t "$ACR_NAME.azurecr.io/$PHOTO_API_IMAGE" \
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

fi

az deployment group create \
	--resource-group $RG_NAME \
	--name 'infra-deployment' \
	--template-file ../infra/main.bicep \
	--parameters ../infra/main.parameters.json \
	--parameters location=$LOCATION \
	--parameters tag=$TAG \
	--parameters acrName=$ACR_NAME \
	--parameters uploadsContainerName=$UPLOADS_CONTAINER_NAME \
	--parameters uploadsStorageQueueName=$UPLOADS_QUEUE_NAME \
	--parameters imagesStorageQueueName=$IMAGES_QUEUE_NAME \
	--parameters imagesContainerName=$IMAGES_CONTAINER_NAME \
	--parameters maxThumbHeight=$MAX_THUMB_HEIGHT \
	--parameters maxThumbWidth=$MAX_THUMB_WIDTH \
	--parameters maxImageHeight=$MAX_IMAGE_HEIGHT \
	--parameters maxImageWidth=$MAX_IMAGE_WIDTH \
	--parameters grpcMaxRequestSizeMb=$GRPC_MAX_REQUEST_SIZE_MB \
	--parameters domainName=$DOMAIN_NAME \
	--parameters subDomainName=$SUBDOMAIN_NAME

# az storage container create -n images --connection-string "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;QueueEndpoint=http://127.0.0.1:10001/devstoreaccount1;"
# az storage container create -n uploads --connection-string "DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==;BlobEndpoint=http://127.0.0.1:10000/devstoreaccount1;QueueEndpoint=http://127.0.0.1:10001/devstoreaccount1;"

