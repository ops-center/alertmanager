# what info sent in between peer 

- Notification logs
    - to ensure cluster sent notification exactly once
    - firing notifications
    - active notifications
    - they take hash of these alerts: https://github.com/prometheus/alertmanager/blob/da8d6803ae66222fd4467205cd090e901355d775/notify/notify.go#L496
    
- silence info


- for HA you have to set all user configs and send alert to all cluster nodes.



### alert flow
- gossip settle : it will wait until cluster is ready, meaning it discovers all the peers: https://github.com/prometheus/alertmanager/blob/7f34cb471671abb3df6e3ef5e918cfa7fc3c4958/cluster/cluster.go#L630
- Inhibit stage
- silence stage
- Wait stage
- Dedup stage - it also checks whether notifications has already sent or not
- Retry stage
- set notifies stage


###  Issue 
- duplicate repeated notifications: https://github.com/prometheus/alertmanager/issues/1005