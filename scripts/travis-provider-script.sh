#!/bin/bash

set -e
script_path=$PWD/$(dirname "$0")

NEXT_VERSION=v$($script_path/get-next-version-by-commit.sh "$TRAVIS_COMMIT_MESSAGE")
git remote set-url --push origin https://${GH_TOKEN}@${GH_REPO}.git
git tag build-${TRAVIS_BUILD_NUMBER}
git tag ${NEXT_VERSION}
git push origin ${TRAVIS_BRANCH} --tags
if [[ $TRAVIS_COMMIT_MESSAGE =~ "_publish_" ]]
then
    docker tag ${REGISTRY_URL}/${DOCKER_IMAGE_NAME} ${DOCKER_IMAGE_NAME}:${NEXT_VERSION}
    $script_path/push-docker-image.sh ${DOCKER_IMAGE_NAME} ${NEXT_VERSION}
fi