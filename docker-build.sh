#!/usr/bin/env bash

pushd $GOPATH/src/github.com/searchlight/alertmanager

./artifacts/format-code.sh

docker build -t nightfury1204/alertmanager:canary .

popd
