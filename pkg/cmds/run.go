package cmds

import (
	"net/http"
	"strings"

	"github.com/searchlight/alertmanager/pkg/alertmanager"

	"github.com/searchlight/alertmanager/pkg/logger"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

func NewCmdRun() *cobra.Command {
	multiAMCfg := &alertmanager.MultitenantAlertmanagerConfig{}

	cmd := &cobra.Command{
		Use:               "run",
		Short:             "Launch alertmanager",
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.InitLogger()
			logger.Logger.Log("msg", "Starting alertmanager")

			if err := multiAMCfg.Validate(); err != nil {
				return err
			}

			amClient := alertmanager.NewInmemAlertmanagerConfigStore()

			multiAM, err := alertmanager.NewMultitenantAlertmanager(multiAMCfg, amClient)
			if err != nil {
				return err
			}
			go multiAM.Run()
			defer multiAM.Stop()

			amAPI := alertmanager.NewAPI(amClient)

			r := mux.NewRouter()
			amAPI.RegisterRoutes(r)
			r.HandleFunc("/api/v1/cluster/status", multiAM.ClusterStatus)

			path := "/" + strings.Trim(multiAMCfg.PathPrefix, "/")

			r.PathPrefix(path).HandlerFunc(multiAM.ServeHTTP)

			// TODO: change the server listen address
			if err := http.ListenAndServe("0.0.0.0:"+multiAMCfg.APIPort, r); err != nil {
				return err
			}
			return nil
		},
	}

	multiAMCfg.AddFlags(cmd.Flags())
	return cmd
}
