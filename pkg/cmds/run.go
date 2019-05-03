package cmds

import (
	"github.com/searchlight/alertmanager/pkg/alertmanager"
	"net/http"

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
			logger.Logger.Log("Starting alertmanager")

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

			r.PathPrefix("/api/prom").HandlerFunc(multiAM.ServeHTTP)
			if err := http.ListenAndServe(multiAMCfg.ClusterBindAddr, r); err != nil {
				return err
			}
			return nil
		},
	}

	multiAMCfg.AddFlags(cmd.Flags())
	return cmd
}
