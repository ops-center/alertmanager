#!/usr/bin/env bash

pushd $GOPATH/src/searchlight.dev/alertmanager/artifacts/demo-webhook

docker build -t searchlight/alert-webhook:canary .

docker push searchlight/alert-webhook:canary

popd
