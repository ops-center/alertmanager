package main

import (
	"github.com/golang/glog"
	"github.com/searchlight/alertmanager/pkg/cmds"
)

func main() {
	if err := cmds.NewRootCmd().Execute(); err != nil {
		glog.Fatal(err)
	}
}
