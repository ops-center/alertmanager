package cmds

import (
	"net/http"
	"strings"

	"go.searchlight.dev/alertmanager/pkg/alertmanager"
	"go.searchlight.dev/alertmanager/pkg/logger"
	"go.searchlight.dev/alertmanager/pkg/storage/etcd"

	"github.com/go-kit/kit/log"
	"github.com/gorilla/mux"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

func NewCmdRun() *cobra.Command {
	multiAMCfg := &alertmanager.MultitenantAlertmanagerConfig{}
	etcdCfg := etcd.NewConfig()

	cmd := &cobra.Command{
		Use:               "run",
		Short:             "Launch alertmanager",
		DisableAutoGenTag: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.InitLogger()
			alertmanager.Must(logger.Logger.Log("msg", "Starting alertmanager"))

			if err := multiAMCfg.Validate(); err != nil {
				return err
			}
			if err := etcdCfg.Validate(); err != nil {
				return err
			}

			etcdClient, err := etcd.NewClient(etcdCfg, log.With(logger.Logger, "domain", "etcd"))
			if err != nil {
				return err
			}

			amGetter, err := alertmanager.NewAlertmanagerGetterWrapper(etcdClient, etcdClient)
			if err != nil {
				return errors.Wrap(err, "failed to create alertmanager getter")
			}

			multiAM, err := alertmanager.NewMultitenantAlertmanager(multiAMCfg, amGetter)
			if err != nil {
				return err
			}
			go multiAM.Run()
			defer multiAM.Stop()

			amAPI := alertmanager.NewAPI(etcdClient)

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
	etcdCfg.AddFlags(cmd.Flags())
	return cmd
}
