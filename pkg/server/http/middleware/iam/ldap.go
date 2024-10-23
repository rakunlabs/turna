package iam

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/ldap"
)

type SyncRequest struct {
	Force bool `json:"force"`
}

// LdapGetGroups returns groups info from LDAP.
// @Summary Get LDAP groups
// @Tags ldap
// @Success 200 {object} data.Response[[]ldap.LdapGroup]
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/groups [GET]
func (m *Iam) LdapGetGroups(w http.ResponseWriter, _ *http.Request) {
	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		httputil.HandleError(w, data.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	groups, err := m.Ldap.Groups(conn)
	if err != nil {
		httputil.HandleError(w, data.NewError("failed getting groups", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[[]ldap.LdapGroup]{
		Meta: &data.Meta{
			TotalItemCount: uint64(len(groups)),
		},
		Payload: groups,
	})
}

// LdapGetUsers returns user info from LDAP.
// @Summary Get LDAP user
// @Tags ldap
// @Param uid path string true "user uid"
// @Success 200 {object} data.Response[ldap.LdapUser]
// @Failure 400 {object} data.ResponseError
// @Failure 404 {object} data.ResponseError
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/users/{uid} [GET]
func (m *Iam) LdapGetUsers(w http.ResponseWriter, r *http.Request) {
	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		httputil.HandleError(w, data.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	uid := chi.URLParam(r, "uid")
	if uid == "" {
		httputil.HandleError(w, data.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	users, err := m.Ldap.Users(conn, []string{uid})
	if err != nil {
		httputil.HandleError(w, data.NewErrorAs(err))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[ldap.LdapUser]{Payload: users[0]})
}

func (m *Iam) LdapSync(force bool, uid string) error {
	m.syncM.Lock()
	defer m.syncM.Unlock()

	slog.Info("syncing ldap starting")
	defer slog.Info("syncing ldap done")

	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		return data.NewError("LDAP connection problem", err, http.StatusInternalServerError)
	}

	groups, err := m.Ldap.Groups(conn)
	if err != nil {
		return data.NewError("failed getting groups", err, http.StatusInternalServerError)
	}

	users := make(map[string][]string)

	if uid != "" {
		users[uid] = nil
	}

	lmapGroups := make([]data.LMapCheckCreate, 0, len(groups))
	for _, group := range groups {
		lmapGroups = append(lmapGroups, data.LMapCheckCreate{
			Name:        group.Name,
			Description: group.Description,
		})

		for _, member := range group.Members {
			if uid != "" && member != uid {
				continue
			}

			users[member] = append(users[member], group.Name)
		}
	}

	// add that users into the database
	return m.db.Update(func(txn *badger.Txn) error {
		// create role (group) if not exists
		if err := m.db.TxCheckCreateLMap(txn, lmapGroups); err != nil {
			return data.NewError("failed creating roles", err, http.StatusInternalServerError)
		}

		roleIDsCache := m.db.LMapRoleIDs()

		for user, groupNames := range users {
			// check if user exists in the database
			userDB, err := m.db.TxGetUser(txn, data.GetUserRequest{Alias: user})
			switch {
			case err == nil:
				roleIDs, err := roleIDsCache.TxGet(txn, groupNames)
				if err != nil {
					return data.NewError("failed getting role IDs", err, http.StatusInternalServerError)
				}

				if !data.CompareSlices(userDB.SyncRoleIDs, roleIDs) {
					// patch user in the database
					userDB.User.SyncRoleIDs = roleIDs
					if err := m.db.TxPutUser(txn, *userDB.User); err != nil {
						return data.NewError("failed updating user", err, http.StatusInternalServerError)
					}
				}

				// ldap user gets one by one so we can skip the rest if not forced
				if !force {
					continue
				}

				// user exists, update it with fetch
				userLdap, err := m.Ldap.Users(conn, []string{user})
				if err != nil {
					return data.NewError("failed getting user", err, http.StatusInternalServerError)
				}

				if len(userLdap) == 0 {
					continue
				}

				for _, u := range userLdap {
					if userDB.User.Details == nil {
						userDB.User.Details = make(map[string]interface{})
					}

					userDB.User.Alias = []string{u.Email, u.UID}
					userDB.User.Details["email"] = u.Email
					userDB.User.Details["uid"] = u.UID
					userDB.User.Details["name"] = u.Name

					userDB.User.SyncRoleIDs = roleIDs

					// update user in the database
					if err := m.db.TxPutUser(txn, *userDB.User); err != nil {
						return data.NewError("failed updating user", err, http.StatusInternalServerError)
					}
				}
			case errors.Is(err, data.ErrNotFound):
				// user not found, add it with fetch
				userLdap, err := m.Ldap.Users(conn, []string{user})
				if err != nil {
					return data.NewError("failed getting user", err, http.StatusInternalServerError)
				}

				if len(userLdap) == 0 {
					continue
				}

				roleIDs, err := roleIDsCache.TxGet(txn, groupNames)
				if err != nil {
					return data.NewError("failed getting role IDs", err, http.StatusInternalServerError)
				}

				for _, u := range userLdap {
					// add user to the database
					id, err := m.db.TxCreateUser(txn, data.User{
						SyncRoleIDs: roleIDs,
						Alias:       []string{u.Email, u.UID},
						Details: map[string]interface{}{
							"email": u.Email,
							"uid":   u.UID,
							"name":  u.Name,
						},
					})
					if err != nil {
						return data.NewError("failed creating user", err, http.StatusInternalServerError)
					}

					slog.Info("user created", slog.String("id", id), slog.String("email", u.Email))
				}
			default:
				return data.NewError("failed getting user", err, http.StatusInternalServerError)
			}
		}

		return nil
	})
}

// LdapSyncGroups syncs users on LDAP groups with mapped groups in the database.
// @Summary Sync LDAP groups
// @Tags ldap
// @Param Body body SyncRequest false "force"
// @Success 200 {object} data.ResponseMessage
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/sync [POST]
func (m *Iam) LdapSyncGroups(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	var req SyncRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, data.NewError("failed decoding request", err, http.StatusBadRequest))
		return
	}

	if err := m.LdapSync(req.Force, ""); err != nil {
		httputil.HandleError(w, data.NewErrorAs(err))
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("Users synced"))
}

// LdapSyncGroups syncs one user on LDAP groups with mapped groups in the database.
// @Summary Sync LDAP groups
// @Tags ldap
// @Param uid path string true "user uid"
// @Param Body body SyncRequest false "force"
// @Success 200 {object} data.ResponseMessage
// @Failure 500 {object} data.ResponseError
// @Router /v1/ldap/sync/{uid} [POST]
func (m *Iam) LdapSyncGroupsUID(w http.ResponseWriter, r *http.Request) {
	if m.sync.Redirect(w, r) {
		return
	}

	uid := chi.URLParam(r, "uid")

	if uid == "" {
		httputil.HandleError(w, data.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	var req SyncRequest
	if err := httputil.Decode(r, &req); err != nil {
		httputil.HandleError(w, data.NewError("failed decoding request", err, http.StatusBadRequest))
		return
	}

	if err := m.LdapSync(req.Force, uid); err != nil {
		httputil.HandleError(w, data.NewErrorAs(err))
	}

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("User synced"))
}
