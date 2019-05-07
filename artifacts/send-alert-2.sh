#!/usr/bin/env bash

alerts1='[
  {
    "labels": {
       "alertname": "DiskRunningFull-2",
       "disk": "sda1"
     }
  },
  {
    "labels": {
       "alertname": "DiskRunningFull-2",
       "disk": "sda2"
     }
  }
]'

curl -X POST \
    -H "X-AppsCode-UserID: 2" \
    -d "$alerts1" \
    http://localhost:9094/api/prom/alertmanager/api/v1/alerts
