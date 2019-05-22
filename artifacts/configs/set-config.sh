#!/usr/bin/env bash

curl -X POST \
    -H "X-AppsCode-UserID: 1" \
    -H "Content-Type: application/json" \
    -d "@/home/ac/go/src/github.com/searchlight/alertmanager/artifacts/configs/user-1.json" \
    http://localhost:19094/api/v1/config

curl -X POST \
    -H "X-AppsCode-UserID: 2" \
    -H "Content-Type: application/json" \
    -d "@/home/ac/go/src/github.com/searchlight/alertmanager/artifacts/configs/user-2.json" \
    http://localhost:19094/api/v1/config
