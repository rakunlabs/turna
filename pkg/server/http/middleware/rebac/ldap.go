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

type SyncRequest struct {
	Force bool `json:"force"`
}

// LdapGetGroups returns groups info from LDAP.
// @Summary Get LDAP groups
// @Tags ldap
// @Success 200 {object} []ldap.LdapGroup
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
// @Success 200 {object} ldap.LdapUser
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

func (m *Rebac) LdapSync(force bool, uid string) error {
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

	users := make(map[string][]string)

	for _, group := range groups {
		for _, member := range group.Members {
			if uid != "" && member != uid {
				continue
			}

			users[member] = append(users[member], group.Name)
		}
	}

	roleIDsCache := m.db.LMapRoleIDs()

	// add that users into the database
	for user, groupNames := range users {
		// check if user exists in the database
		userDB, err := m.db.GetUser(data.GetUserRequest{Alias: user})
		switch {
		case err == nil:
			if !data.CompareSlices(userDB.SyncRoleIDs, groupNames) {
				roleIDs, err := roleIDsCache.Get(groupNames)
				if err != nil {
					return httputil.NewError("failed getting role IDs", err, http.StatusInternalServerError)
				}

				// patch user in the database
				userDB.User.SyncRoleIDs = roleIDs
				if err := m.db.PutUser(userDB.User); err != nil {
					return httputil.NewError("failed updating user", err, http.StatusInternalServerError)
				}
			}

			// ldap user gets one by one so we can skip the rest if not forced
			if !force {
				continue
			}

			// user exists, update it with fetch
			userLdap, err := m.Ldap.Users(conn, []string{user})
			if err != nil {
				return httputil.NewError("failed getting user", err, http.StatusInternalServerError)
			}

			if len(userLdap) == 0 {
				continue
			}

			roleIDs, err := roleIDsCache.Get(groupNames)
			if err != nil {
				return httputil.NewError("failed getting role IDs", err, http.StatusInternalServerError)
			}

			for _, u := range userLdap {
				userDB.User.Alias = []string{u.Email, u.UID}
				userDB.User.Details["email"] = u.Email
				userDB.User.Details["uid"] = u.UID
				userDB.User.Details["name"] = u.Name

				userDB.User.SyncRoleIDs = roleIDs

				// update user in the database
				if err := m.db.PutUser(userDB.User); err != nil {
					return httputil.NewError("failed updating user", err, http.StatusInternalServerError)
				}
			}
		case errors.Is(err, data.ErrNotFound):
			// user not found, add it with fetch
			userLdap, err := m.Ldap.Users(conn, []string{user})
			if err != nil {
				return httputil.NewError("failed getting user", err, http.StatusInternalServerError)
			}

			if len(userLdap) == 0 {
				continue
			}

			roleIDs, err := roleIDsCache.Get(groupNames)
			if err != nil {
				return httputil.NewError("failed getting role IDs", err, http.StatusInternalServerError)
			}

			for _, u := range userLdap {
				// add user to the database
				if err := m.db.CreateUser(data.User{
					ID:      ulid.Make().String(),
					RoleIDs: roleIDs,
					Alias:   []string{u.Email, u.UID},
					Details: map[string]interface{}{
						"email": u.Email,
						"uid":   u.UID,
						"name":  u.Name,
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
// @Param Body body SyncRequest false "force"
// @Success 200 {object} httputil.Response
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/sync [POST]
func (m *Rebac) LdapSyncGroups(w http.ResponseWriter, r *http.Request) {
	var req SyncRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("failed decoding request", err, http.StatusBadRequest))
		return
	}

	if err := m.LdapSync(req.Force, ""); err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{Msg: "Users synced"})
}

// LdapSyncGroups syncs one user on LDAP groups with mapped groups in the database.
// @Summary Sync LDAP groups
// @Tags ldap
// @Param uid path string true "user uid"
// @Param Body body SyncRequest false "force"
// @Success 200 {object} httputil.Response
// @Failure 500 {object} httputil.Error
// @Router /v1/ldap/sync/{uid} [POST]
func (m *Rebac) LdapSyncGroupsUID(w http.ResponseWriter, r *http.Request) {
	uid := chi.URLParam(r, "uid")

	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	var req SyncRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, httputil.NewError("failed decoding request", err, http.StatusBadRequest))
		return
	}

	if err := m.LdapSync(req.Force, uid); err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
	}

	httputil.JSON(w, http.StatusOK, httputil.Response{Msg: "Users synced"})
}
