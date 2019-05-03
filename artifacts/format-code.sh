#!/usr/bin/env bash

pushd $GOPATH/src/github.com/searchlight/alertmanager

gofmt -s -w *.go pkg

goimports -w *.go pkg

popd