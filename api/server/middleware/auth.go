package middleware

import (
	"context"
	"net/http"
	"regexp"

	"github.com/Sirupsen/logrus"
	"github.com/cloudway/platform/api/server/httputils"
	"github.com/cloudway/platform/broker"
)

type authMiddleware struct {
	*broker.Broker
	noAuthPattern *regexp.Regexp
}

func NewAuthMiddleware(broker *broker.Broker, contextRoot string) authMiddleware {
	pattern := regexp.MustCompile("^" + contextRoot + "(/v[0-9.]+)?/(version|auth|swagger.json)")
	return authMiddleware{broker, pattern}
}

func (m authMiddleware) WrapHandler(handler httputils.APIFunc) httputils.APIFunc {
	return func(w http.ResponseWriter, r *http.Request, vars map[string]string) error {
		if m.noAuthPattern.MatchString(r.URL.Path) {
			return handler(w, r, vars)
		}

		user, err := m.Authz.Verify(r)
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return nil
		}

		logrus.Debugf("Logged in user: %s", user)
		ctx := context.WithValue(r.Context(), httputils.UserKey, user)
		return handler(w, r.WithContext(ctx), vars)
	}
}
