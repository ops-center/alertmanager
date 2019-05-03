package alertmanager

import (
	"net/http"

	"github.com/pkg/errors"
)

const (
	// UserIDHeaderName denotes the UserID the request has been authenticated as
	UserIDHeaderName = "X-AppsCode-UserID"
)

func ExtractUserIDFromHTTPRequest(r *http.Request) (string, error) {
	uid := r.Header.Get(UserIDHeaderName)
	if uid == "" {
		return "", errors.New("user id is not provided")
	}
	return uid, nil
}

func SetUserIDInHTTPRequest(userID string, r *http.Request) error {
	uid := r.Header.Get(UserIDHeaderName)
	if uid != "" && uid != userID {
		return errors.New("existing userID didn't match with given userID")
	}
	r.Header.Set(UserIDHeaderName, userID)
	return nil
}
