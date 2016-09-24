package middleware

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cloudway/platform/api"
	"github.com/cloudway/platform/api/server/httputils"
	"github.com/cloudway/platform/broker"
)

type badRequestError struct {
	error
}

func (badRequestError) HTTPErrorStatusCode() int {
	return http.StatusBadRequest
}

// VersionMiddleware is a middleware that validates the client and server versions.
type VersionMiddleware struct {
	*broker.Broker
	dockerVersion string
}

// NewVersionMiddleware creates a new VersionMiddleware with the default versions
func NewVersionMiddleware(broker *broker.Broker) VersionMiddleware {
	return VersionMiddleware{Broker: broker}
}

// WrapHandler returns a new handler function wrapping the previous one in the request chain
func (m VersionMiddleware) WrapHandler(handler httputils.APIFunc) httputils.APIFunc {
	return func(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
		apiVersion := vars["version"]
		if apiVersion == "" {
			apiVersion = api.Version
		}

		if api.CompareVersions(apiVersion, api.Version) > 0 {
			return badRequestError{
				fmt.Errorf("client is newer than server (client API version: %s, server API version: %s)",
					apiVersion, api.Version),
			}
		}
		if api.CompareVersions(apiVersion, api.MinVersion) < 0 {
			return badRequestError{
				fmt.Errorf("client version %s is too old. Minimum supported API version is %s, "+
					"please upgrade your client to a newer version", apiVersion, api.MinVersion),
			}
		}

		if m.dockerVersion == "" {
			v, err := m.ServerVersion(r.Context())
			if err == nil {
				m.dockerVersion = v.Version
			}
		}

		var header string
		if m.dockerVersion != "" {
			header = fmt.Sprintf("Cloudway-API/%s Docker/%s", api.Version, m.dockerVersion)
		} else {
			header = fmt.Sprintf("Cloudway-API/%s", api.Version)
		}

		w.Header().Set("Server", header)
		ctx := context.WithValue(r.Context(), httputils.APIVersionKey, apiVersion)
		return handler(w, r.WithContext(ctx), vars)
	}
}
