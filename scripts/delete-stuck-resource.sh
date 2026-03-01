k get < resource-type > < name > -o json > < name >.json
kubectl replace --raw "/api/v1/namespaces/< name >/finalize" -f ./< name >.json