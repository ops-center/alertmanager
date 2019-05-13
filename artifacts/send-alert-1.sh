#!/usr/bin/env bash

alerts1='[
  {
    "labels": {
       "alertname": "DiskRunningFull-1",
       "dev": "sda1",
       "instance": "example1"
     },
     "annotations": {
        "info": "The disk sda1 is running full",
        "summary": "please check the instance example1"
      }
  },
  {
    "labels": {
       "alertname": "DiskRunningFull-1",
       "dev": "sda2",
       "instance": "example1"
     },
     "annotations": {
        "info": "The disk sda2 is running full",
        "summary": "please check the instance example1",
        "runbook": "the following link http://test-url should be clickable"
      }
  }
]'

curl -X POST \
    -H "X-AppsCode-UserID: 1" \
    -d "$alerts1" \
    http://localhost:9094/api/prom/alertmanager/api/v1/alerts

curl -X POST \
    -H "X-AppsCode-UserID: 1" \
    -d "$alerts1" \
    http://localhost:9096/api/prom/alertmanager/api/v1/alerts
