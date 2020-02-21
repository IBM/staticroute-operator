#!/bin/bash

DOCKER_IMAGE=$1
DOCKER_TAG=$2
PID_LIST=()
EXIT_CODE=0
TIMEOUT=60
REGISTRIES=$(echo "${DOCKER_REGISTRY_LIST}" | tr ',' ' ')

for docker_registry in ${REGISTRIES}
do
  registry_token_name=$(echo ${docker_registry}_token | sed -e "s#[.|-]#_#g")
  token="${!registry_token_name}"
  echo "Preparing to push to ${docker_registry}..."
  docker login --username ${DOCKER_USERNAME} --password-stdin ${docker_registry} <<<${token}
  docker tag ${DOCKER_IMAGE}:${DOCKER_TAG} ${docker_registry}/${DOCKER_IMAGE}:${DOCKER_TAG}
  docker push ${docker_registry}/${DOCKER_IMAGE}:${DOCKER_TAG} > /tmp/${docker_registry}.log 2>&1 &
  DOCKER_PID_LIST+=($!)
done

# keep Travis alive, when pushing large docker images
while (( TIMEOUT-- > 0 ))
do
  echo "Pushing ${DOCKER_IMAGE}:${DOCKER_TAG} image to [${DOCKER_REGISTRY_LIST}] registries..."
  sleep 60
done &
KEEPALIVE_PID=$!

for pid in "${DOCKER_PID_LIST[@]}"
do
  wait ${pid} || (( EXIT_CODE+=1 ))
done

kill $KEEPALIVE_PID

for docker_registry in ${REGISTRIES}
do
  echo && cat /tmp/${docker_registry}.log
done

exit ${EXIT_CODE}
