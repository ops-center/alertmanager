#!/usr/bin/env bash

pushd $GOPATH/src/github.com/searchlight/alertmanager/artifacts/demo-webhook

docker build -t nightfury1204/alert-webhook:canary .

docker push nightfury1204/alert-webhook:canary

popd
