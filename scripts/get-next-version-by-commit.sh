#! /bin/bash

: ${1?= Commit message is required}

VERSION=$(git tag -l "v[0-9]*" | grep -oE "v[[:digit:]]+\.[[:digit:]]+\.[[:digit:]]+" | sort -rV | head -1 | tr -d "v")
VERSION=${VERSION:-0.0.0} #for the very first tag
CHANGE_TYPE=$(echo $1 | grep -o '_major_\|_minor_\|_patch_')

case $CHANGE_TYPE in
_major_)
  echo $VERSION | awk '{split($0,a,"."); print ++a[1] "." 0 "." 0}'
  ;;
_minor_)
  echo $VERSION | awk '{split($0,a,"."); print a[1] "." ++a[2] "." 0}'
  ;;
_patch_)
  echo $VERSION | awk '{split($0,a,"."); print a[1] "." a[2] "." ++a[3]}'
  ;;
*)
  echo -n ""
  exit 1
  ;;
esac
