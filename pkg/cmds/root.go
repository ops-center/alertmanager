package cmds

import (
	"flag"

	"searchlight.dev/alertmanager/pkg/alertmanager"

	"github.com/spf13/cobra"
)

func NewRootCmd() *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:               "alertmanager [command]",
		Short:             `alertmanager for m3db`,
		DisableAutoGenTag: true,
	}

	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	// ref: https://github.com/kubernetes/kubernetes/issues/17162#issuecomment-225596212
	alertmanager.Must(flag.CommandLine.Parse([]string{}))
	rootCmd.AddCommand(NewCmdRun())

	return rootCmd
}
