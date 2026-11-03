package web

import (
	"context"
	"log/slog"
	"net/http"
	"slices"

	"github.com/alex0ptr/aigude/pkg/userconfig"
)

// userHeader is the header a proxy in front sets to the Id of the Pinned User. Its name is fixed.
const userHeader = "X-Aigude-User"

// demoUserID is the Id of the User that is shown in Demo Mode: whoever looks at the pages of this User can browse and
// play, and nothing can be changed.
const demoUserID = "demo"

// access is what is known about a request before a handler runs: the User Config in force, whom the proxy pinned
// and whether the request is in Demo Mode, which is the case when the selected User is the demo User. Every handler works with it, so a reload of the User Config in the middle
// of a request cannot show a mix of old and new.
type access struct {
	snapshot *userconfig.Snapshot
	// pinned is the User the proxy named; nil when the request is not pinned.
	pinned *userconfig.User
	// demo tells that the request is in Demo Mode and can only read, because it is for the demo User.
	demo bool
}

// operatorPaths are the pages of the person running the service, the log and the editor of the User Config. Demo Mode
// refuses them, though they only read.
var operatorPaths = []string{"/log", "/config"}

type accessKey struct{}

// accessOf returns the access the middleware determined for a request.
func accessOf(r *http.Request) access {
	return r.Context().Value(accessKey{}).(access)
}

// withAccess is the one place that decides who is asking and what they may do. It answers 403 for a pinned User that is
// not known, and in Demo Mode for everything that is not reading and for the operatorPaths; the only write that stays
// is the choice of a User when none is pinned.
func (h *Handler) withAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := access{snapshot: h.source.Snapshot()}
		if values := r.Header.Values(userHeader); len(values) > 0 {
			user, ok := a.snapshot.User(values[0])
			if !ok || slices.ContainsFunc(values, func(v string) bool { return v != values[0] }) {
				slog.Warn("Request for a Pinned User that is not known refused", "header", userHeader, "user", values[0], "path", r.URL.Path)
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			a.pinned = &user
		}
		if user := a.selectedUser(r); user != nil && user.ID == demoUserID {
			a.demo = true
		}
		choosesUser := r.Method == http.MethodPost && r.URL.Path == "/user"
		reading := r.Method == http.MethodGet || r.Method == http.MethodHead
		operatorOnly := slices.Contains(operatorPaths, r.URL.Path)
		if (a.pinned != nil && choosesUser) || (a.demo && !reading && !choosesUser) || (a.demo && operatorOnly) {
			slog.Info("Request refused", "method", r.Method, "path", r.URL.Path, "pinned", a.pinned != nil, "demo", a.demo)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), accessKey{}, a)))
	})
}

// selectedUser returns the User whose pages the request shows: the Pinned User, else the User remembered in the
// cookie, else the first User; nil when there are no Users.
func (a access) selectedUser(r *http.Request) *userconfig.User {
	if a.pinned != nil {
		return a.pinned
	}
	users := a.snapshot.Users()
	if len(users) == 0 {
		return nil
	}
	if c, err := r.Cookie(userCookie); err == nil {
		if u, ok := a.snapshot.User(c.Value); ok {
			return &u
		}
		slog.Debug("Remembered User is unknown, using the first", "user", c.Value)
	}
	return &users[0]
}
