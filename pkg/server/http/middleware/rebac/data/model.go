package data

import (
	"errors"
	"io"
)

var (
	ErrConflict       = errors.New("conflict")
	ErrNotFound       = errors.New("not found")
	ErrInvalidRequest = errors.New("invalid request")
)

// Permission is a struct that represents a permission table in the database.
type Permission struct {
	ID          string     `json:"id"          badgerhold:"unique"`
	Name        string     `json:"name"        badgerhold:"index"`
	Resources   []Resource `json:"resources"`
	Description string     `json:"description"`
}

type Resource struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// Role is a struct that represents a role table in the database.
type Role struct {
	ID            string                 `json:"id"             badgerhold:"unique"`
	Name          string                 `json:"name"           badgerhold:"index"`
	PermissionIDs []string               `json:"permission_ids"`
	RoleIDs       []string               `json:"role_ids"`
	Data          map[string]interface{} `json:"data"`
	Description   string                 `json:"description"`
}

type RoleExtended struct {
	*Role

	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	TotalUsers  uint64   `json:"total_users,omitempty"`
}

type PermissionIDs struct {
	PermissionIDs []string `json:"permission_ids"`
}

type RoleIDs struct {
	RoleIDs []string `json:"role_ids"`
}

// User is a struct that represents a user table in the database.
type User struct {
	ID          string                 `json:"id"            badgerhold:"unique"`
	Alias       []string               `json:"alias"`
	RoleIDs     []string               `json:"role_ids"`
	SyncRoleIDs []string               `json:"sync_role_ids"`
	Details     map[string]interface{} `json:"details"`
}

type UserExtended struct {
	*User

	Roles       []string      `json:"roles,omitempty"`
	Permissions []string      `json:"permissions,omitempty"`
	Datas       []interface{} `json:"datas,omitempty"`
}

type UserInfo struct {
	Details     map[string]interface{} `json:"details"`
	Roles       []string               `json:"roles,omitempty"`
	Permissions []string               `json:"permissions,omitempty"`
	Datas       []interface{}          `json:"datas,omitempty"`
}

type LMap struct {
	Name    string   `json:"name"     badgerhold:"unique"`
	RoleIDs []string `json:"role_ids"`
}

// //////////////////////////////////////////////////////////////////////

type Response[T any] struct {
	Message *Message `json:"message,omitempty"`
	Meta    *Meta    `json:"meta,omitempty"`
	Payload T        `json:"payload,omitempty"`
}

type ResponseMessage struct {
	Message *Message `json:"message,omitempty"`
}

func NewResponseMessage(text string) ResponseMessage {
	return ResponseMessage{
		Message: &Message{
			Text: text,
		},
	}
}

type Meta struct {
	Offset         int64  `json:"offset,omitempty"`
	Limit          int64  `json:"limit,omitempty"`
	TotalItemCount uint64 `json:"total_item_count,omitempty"`
}

type Message struct {
	Text string `json:"text,omitempty"`
	Err  string `json:"error,omitempty"`
}

type ResponseCreate struct {
	ID string `json:"id"`
}

// //////////////////////////////////////////////////////////////////////

type GetUserRequest struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`

	RoleIDs []string `json:"role_ids"`

	UID   string `json:"uid"`
	Name  string `json:"name"`
	Email string `json:"email"`

	Path   string `json:"path"`
	Method string `json:"method"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`

	AddRoles       bool `json:"add_role"`
	AddPermissions bool `json:"add_permissions"`
	AddDatas       bool `json:"add_datas"`
}

type GetPermissionRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Path   string `json:"path"`
	Method string `json:"method"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`
}

type GetRoleRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	PermissionIDs []string `json:"permission_ids"`
	RoleIDs       []string `json:"role_ids"`

	Path   string `json:"path"`
	Method string `json:"method"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`

	AddPermissions bool `json:"add_permissions"`
	AddRoles       bool `json:"add_roles"`
	AddTotalUsers  bool `json:"add_total_users"`
}

type GetLMapRequest struct {
	Name string `json:"name"`

	RoleIDs []string `json:"role_ids"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`
}

type CheckRequest struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`

	Path   string `json:"path"`
	Method string `json:"method"`
}

type CheckResponse struct {
	Allowed bool `json:"allowed"`
}

type LMapRoleIDs interface {
	Get(names []string) ([]string, error)
}

type Database interface {
	GetUsers(req GetUserRequest) (*Response[[]UserExtended], error)
	GetUser(req GetUserRequest) (*UserExtended, error)
	CreateUser(user User) (string, error)
	DeleteUser(id string) error
	PutUser(user User) error
	PatchUser(user User) error
	AddUserRole(id string, roles RoleIDs) error
	DeleteUserRole(id string, roles RoleIDs) error

	GetPermissions(req GetPermissionRequest) (*Response[[]Permission], error)
	GetPermission(id string) (*Permission, error)
	CreatePermission(permission Permission) (string, error)
	DeletePermission(id string) error
	PutPermission(permission Permission) error
	PatchPermission(permission Permission) error

	GetRoles(req GetRoleRequest) (*Response[[]RoleExtended], error)
	GetRole(req GetRoleRequest) (*RoleExtended, error)
	CreateRole(role Role) (string, error)
	PutRole(role Role) error
	DeleteRole(id string) error
	PatchRole(role Role) error
	AddRolePermission(id string, permissions PermissionIDs) error
	DeleteRolePermission(id string, permissions PermissionIDs) error
	AddRoleRole(id string, roles RoleIDs) error
	DeleteRoleRole(id string, roles RoleIDs) error

	Check(req CheckRequest) (*CheckResponse, error)

	GetLMaps(req GetLMapRequest) (*Response[[]LMap], error)
	GetLMap(name string) (*LMap, error)
	CreateLMap(lmap LMap) error
	PutLMap(lmap LMap) error
	DeleteLMap(name string) error

	LMapRoleIDs() LMapRoleIDs

	Backup(w io.Writer, since uint64) error
	Restore(r io.Reader) error
}

func CompareSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]struct{}, len(a))
	for _, v := range a {
		aMap[v] = struct{}{}
	}

	for _, v := range b {
		if _, ok := aMap[v]; !ok {
			return false
		}
	}

	return true
}
