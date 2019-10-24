package main

import (
	"searchlight.dev/alertmanager/pkg/cmds"

	"github.com/golang/glog"
)

func main() {
	if err := cmds.NewRootCmd().Execute(); err != nil {
		glog.Fatal(err)
	}
}
