package iam

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/dgraph-io/badger/v4"
	"github.com/go-chi/chi/v5"

	"github.com/rakunlabs/logi"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/ldap"
)

type SyncRequest struct {
	Force bool `json:"force"`
}

func (m *Iam) LdapCheckPassword(username, password string) (bool, error) {
	v, err := m.Ldap.CheckPassword(username, password)
	if err != nil {
		return false, err
	}

	return v, nil
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
		httputil.HandleError(w, httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	groups, err := m.Ldap.Groups(conn)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("failed getting groups", err, http.StatusInternalServerError))
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
		httputil.HandleError(w, httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	uid := chi.URLParam(r, "uid")
	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	uid, err = url.PathUnescape(uid)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("failed unescaping uid", err, http.StatusBadRequest))
		return
	}

	users, err := m.Ldap.Users(conn, []string{uid})
	if err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[ldap.LdapUser]{Payload: users[0]})
}

func (m *Iam) LdapSync(force bool, uid string) error {
	m.syncM.Lock()
	defer m.syncM.Unlock()

	logi.Ctx(m.ctxService).Info("syncing ldap starting")
	defer logi.Ctx(m.ctxService).Info("syncing ldap done")

	ctx := data.WithContextUserName(m.ctxService, "LDAP")

	conn, err := m.Ldap.ConnectWithCheck()
	if err != nil {
		return httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError)
	}

	groups, err := m.Ldap.Groups(conn)
	if err != nil {
		return httputil.NewError("failed getting groups", err, http.StatusInternalServerError)
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
		if err := m.db.TxCheckCreateLMap(ctx, txn, lmapGroups); err != nil {
			return httputil.NewError("failed creating roles", err, http.StatusInternalServerError)
		}

		roleIDsCache := m.db.LMapRoleIDs()

		for user, groupNames := range users {
			// check if user exists in the database
			userDB, err := m.db.TxGetUser(txn, data.GetUserRequest{Alias: user})
			switch {
			case err == nil:
				roleIDs, err := roleIDsCache.TxGet(txn, groupNames)
				if err != nil {
					return httputil.NewError("failed getting role IDs", err, http.StatusInternalServerError)
				}

				if !data.CompareSlices(userDB.SyncRoleIDs, roleIDs) {
					// patch user in the database
					userDB.User.SyncRoleIDs = roleIDs
					if err := m.db.TxPutUser(ctx, txn, *userDB.User); err != nil {
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
					if err := m.db.TxPutUser(ctx, txn, *userDB.User); err != nil {
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

				roleIDs, err := roleIDsCache.TxGet(txn, groupNames)
				if err != nil {
					return httputil.NewError("failed getting role IDs", err, http.StatusInternalServerError)
				}

				for _, u := range userLdap {
					// add user to the database
					_, err := m.db.TxCreateUser(ctx, txn, data.User{
						SyncRoleIDs: roleIDs,
						Alias:       []string{u.Email, u.UID},
						Details: map[string]interface{}{
							"email": u.Email,
							"uid":   u.UID,
							"name":  u.Name,
						},
					})
					if err != nil {
						return httputil.NewError("failed creating user", err, http.StatusInternalServerError)
					}
				}
			default:
				return httputil.NewError("failed getting user", err, http.StatusInternalServerError)
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
		httputil.HandleError(w, httputil.NewError("failed decoding request", err, http.StatusBadRequest))
		return
	}

	if err := m.LdapSync(req.Force, ""); err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
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

	m.sync.Trigger(m.ctxService)

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("User synced"))
}

func (m *Iam) GetOrCreateUser(request data.GetUserRequest) (*data.UserExtended, error) {
	user, err := m.db.GetUser(request)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			if err := m.LdapSync(false, request.Alias); err != nil {
				return nil, err
			}

			user, err = m.db.GetUser(request)
			if err != nil {
				return nil, err
			}

			return user, nil
		}

		return nil, err
	}

	return user, nil
}
