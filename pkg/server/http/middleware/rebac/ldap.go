package rebac

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
)

// LdapGetGroups returns groups info from LDAP.
// @Summary Get LDAP groups
// @Tags ldap
// @Success 200 {object} []LdapGroup
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/groups [GET]
func (m *Rebac) LdapGetGroups(w http.ResponseWriter, _ *http.Request) {
	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	groups, err := m.Ldap.Groups(conn)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("failed getting groups", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, groups)
}

// LdapGetUsers returns user info from LDAP.
// @Summary Get LDAP user
// @Tags ldap
// @Param uid path string true "user uid"
// @Success 200 {object} LdapUser
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/users/{uid} [GET]
func (m *Rebac) LdapGetUsers(w http.ResponseWriter, r *http.Request) {
	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	uid := chi.URLParam(r, "uid")
	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	users, err := m.Ldap.Users(conn, []string{uid})
	if err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
		return
	}

	httputil.JSON(w, http.StatusOK, users[0])
}

func (m *Rebac) LdapSync() error {
	slog.Info("syncing ldap starting")
	defer slog.Info("syncing ldap done")

	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		return httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError)
	}

	groups, err := m.Ldap.Groups(conn)
	if err != nil {
		return httputil.NewError("failed getting groups", err, http.StatusInternalServerError)
	}

	users := make(map[string]struct{})

	for _, group := range groups {
		for _, member := range group.Members {
			users[member] = struct{}{}
		}
	}

	// add that users into the database
	for user := range users {
		// check if user exists in the database
		_, err := m.db.GetUser(data.GetUserRequest{Alias: user})
		switch {
		case err == nil:
			continue
		case errors.Is(err, data.ErrNotFound):
			// user not found, add it with fetch
			userLdap, err := m.Ldap.Users(conn, []string{user})
			if err != nil {
				return httputil.NewError("failed getting user", err, http.StatusInternalServerError)
			}

			if len(userLdap) == 0 {
				continue
			}

			for _, u := range userLdap {
				// add user to the database
				if err := m.db.CreateUser(data.User{
					ID:    ulid.Make().String(),
					Alias: []string{u.Email, u.UID},
					Details: map[string]interface{}{
						"email":        u.Email,
						"display_name": u.DisplayName,
						"uid":          u.UID,
						"gecos":        u.Gecos,
					},
				}); err != nil {
					return httputil.NewError("failed creating user", err, http.StatusInternalServerError)
				}
			}
		default:
			return httputil.NewError("failed getting user", err, http.StatusInternalServerError)
		}
	}

	return nil
}

// LdapSyncGroups syncs users on LDAP groups with mapped groups in the database.
// @Summary Sync LDAP groups
// @Tags ldap
// @Success 200 {object} httputil.Response
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/sync [POST]
func (m *Rebac) LdapSyncGroups(w http.ResponseWriter, _ *http.Request) {
	if err := m.LdapSync(); err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{Msg: "Users synced"})
}
