#!/bin/bash

set -e
script_path=$PWD/$(dirname "$0")

NEXT_VERSION=v$($script_path/get-next-version-by-commit.sh "$GITHUB_COMMIT_MESSAGE")
if [[ $GITHUB_COMMIT_MESSAGE =~ "_publish_" ]]
then
    docker tag ${REGISTRY_URL}/${DOCKER_IMAGE_NAME} ${DOCKER_IMAGE_NAME}:${NEXT_VERSION}
    $script_path/push-docker-image.sh ${DOCKER_IMAGE_NAME} ${NEXT_VERSION}
fi

# If publish was successful, tag the commit and push tags.
git remote set-url --push origin https://${GH_TOKEN}@${GH_REPO}.git
git tag ${NEXT_VERSION}-${GITHUB_RUN_NUMBER}
git push origin ${GITHUB_BRANCH} --tags
