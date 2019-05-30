package alertmanager

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/gorilla/mux"
	amconfig "github.com/prometheus/alertmanager/config"
	logger2 "github.com/searchlight/alertmanager/pkg/logger"
)

// API implements the configs api.
type API struct {
	client AlertmanagerClient
	http.Handler
}

// New creates a new API
func NewAPI(c AlertmanagerClient) *API {
	a := &API{client: c}
	r := mux.NewRouter()
	a.RegisterRoutes(r)
	a.Handler = r
	return a
}

// RegisterRoutes registers the configs API HTTP routes with the provided Router.
func (a *API) RegisterRoutes(r *mux.Router) {
	for _, route := range []struct {
		name, method, path string
		handler            http.HandlerFunc
	}{
		{"get_config", "GET", "/api/v1/config", a.getConfig},
		{"set_config", "POST", "/api/v1/config", a.setConfig},
		{"deactivate_config", "DELETE", "/api/v1/config/deactivate", a.deactivateConfig},
		{"restore_config", "POST", "/api/v1/config/restore", a.restoreConfig},
	} {
		r.Handle(route.path, route.handler).Methods(route.method).Name(route.name)
	}
}

// getConfig returns the request configuration.
func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	userID, err := ExtractUserIDFromHTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	logger := logger2.WithUserID(userID, logger2.Logger)

	cfg, err := a.client.GetConfig(userID)
	if err != nil {
		// XXX: Untested
		level.Error(logger).Log("msg", "error getting config", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		// XXX: Untested
		level.Error(logger).Log("msg", "error encoding config", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (a *API) setConfig(w http.ResponseWriter, r *http.Request) {
	userID, err := ExtractUserIDFromHTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// logger with userID
	logger := logger2.WithUserID(userID, logger2.Logger)

	var cfg AlertmanagerConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		// XXX: Untested
		level.Error(logger).Log("msg", "error decoding json body", "err", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateAlertmanagerConfig(cfg.Config); err != nil {
		level.Error(logger).Log("msg", "invalid Alertmanager config", "err", err)
		http.Error(w, fmt.Sprintf("Invalid Alertmanager config: %v", err), http.StatusBadRequest)
		return
	}

	if err := validateTemplateFiles(cfg.TemplateFiles); err != nil {
		level.Error(logger).Log("msg", "invalid templates", "err", err)
		http.Error(w, fmt.Sprintf("Invalid templates: %v", err), http.StatusBadRequest)
		return
	}

	cfg.UserID = userID
	cfg.UpdatedAtInUnix = time.Now().Unix()
	if err := a.client.SetConfig(&cfg); err != nil {
		// XXX: Untested
		level.Error(logger).Log("msg", "error storing config", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deactivateConfig(w http.ResponseWriter, r *http.Request) {
	userID, err := ExtractUserIDFromHTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	// logger with userID
	logger := logger2.WithUserID(userID, logger2.Logger)

	if err := a.client.DeactivateConfig(userID); err != nil {
		level.Error(logger).Log("msg", "error deactivating config", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	level.Info(logger).Log("msg", "config deactivated", "userID", userID)
	w.WriteHeader(http.StatusOK)
}

func (a *API) restoreConfig(w http.ResponseWriter, r *http.Request) {
	userID, err := ExtractUserIDFromHTTPRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// logger with userID
	logger := logger2.WithUserID(userID, logger2.Logger)

	if err := a.client.RestoreConfig(userID); err != nil {
		level.Error(logger).Log("msg", "error restoring config", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	level.Info(logger).Log("msg", "config restored", "userID", userID)
	w.WriteHeader(http.StatusOK)
}

func validateAlertmanagerConfig(cfg string) error {
	// TODO: should check for templates files
	_, err := amconfig.Load(cfg)
	if err != nil {
		return err
	}
	return nil
}

func validateTemplateFiles(tplFiles map[string]string) error {
	for fn, content := range tplFiles {
		if _, err := template.New(fn).Parse(content); err != nil {
			return err
		}
	}
	return nil
}
