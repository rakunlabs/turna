package data

import "errors"

var (
	ErrConflict       = errors.New("conflict")
	ErrNotFound       = errors.New("not found")
	ErrInvalidRequest = errors.New("invalid request")
)

// Permission is a struct that represents a permission table in the database.
type Permission struct {
	ID          string    `json:"id"          badgerhold:"unique"`
	Name        string    `json:"name"`
	Requests    []Request `json:"requests"`
	Description string    `json:"description"`
}

type Request struct {
	Path    string   `json:"path"`
	Methods []string `json:"methods"`
}

// Role is a struct that represents a role table in the database.
type Role struct {
	ID            string                 `json:"id"             badgerhold:"unique"`
	Name          string                 `json:"name"`
	PermissionIDs []string               `json:"permission_ids"`
	RoleIDs       []string               `json:"role_ids"`
	Data          map[string]interface{} `json:"data"`
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
	User

	Roles       []string      `json:"roles,omitempty"`
	Permissions []string      `json:"permissions,omitempty"`
	Datas       []interface{} `json:"datas,omitempty"`
}

type LMap struct {
	Name    string   `json:"name"     badgerhold:"unique"`
	RoleIDs []string `json:"role_ids"`
}

// //////////////////////////////////////////////////////////////////////

type Response[T any] struct {
	Meta    Meta `json:"meta"`
	Payload T    `json:"payload"`
}

type Meta struct {
	Offset         int64  `json:"offset,omitempty"`
	Limit          int64  `json:"limit,omitempty"`
	TotalItemCount uint64 `json:"total_item_count,omitempty"`
}

type ResponseCreate struct {
	ID      string `json:"id"`
	Message string `json:"message"`
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

	Extend bool `json:"extend"`
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
	CreateUser(user User) error
	DeleteUser(id string) error
	PutUser(user User) error
	PatchUser(user User) error

	GetPermissions(req GetPermissionRequest) (*Response[[]Permission], error)
	GetPermission(id string) (*Permission, error)
	CreatePermission(permission Permission) error
	DeletePermission(id string) error
	PutPermission(permission Permission) error
	PatchPermission(permission Permission) error

	GetRoles(req GetRoleRequest) (*Response[[]Role], error)
	GetRole(id string) (*Role, error)
	CreateRole(role Role) error
	PutRole(role Role) error
	DeleteRole(id string) error
	PatchRole(role Role) error

	Check(req CheckRequest) (*CheckResponse, error)

	GetLMaps(req GetLMapRequest) (*Response[[]LMap], error)
	GetLMap(name string) (*LMap, error)
	CreateLMap(lmap LMap) error
	PutLMap(lmap LMap) error
	DeleteLMap(name string) error

	LMapRoleIDs() LMapRoleIDs
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
