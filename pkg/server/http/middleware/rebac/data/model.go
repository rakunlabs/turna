package data

import "errors"

var (
	ErrConflict = errors.New("conflict")
	ErrNotFound = errors.New("not found")
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
	ID          string   `json:"id"          badgerhold:"unique"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// User is a struct that represents a user table in the database.
type User struct {
	ID      string                 `json:"id"      badgerhold:"unique"`
	Alias   []string               `json:"alias"`
	Roles   []string               `json:"roles"`
	Details map[string]interface{} `json:"details"`
}

type UserExtended struct {
}

type LMap struct {
	Name string `json:"name" badgerhold:"unique"`
	Role string `json:"role"`
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

// //////////////////////////////////////////////////////////////////////

type GetUserRequest struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`
}

type GetPermissionRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`
}

type GetRoleRequest GetPermissionRequest

type GetLMapRequest GetPermissionRequest

type CheckRequest struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`

	Path   string `json:"path"`
	Method string `json:"method"`
}

type CheckResponse struct {
	Allowed bool `json:"allowed"`
}

type Database interface {
	GetUsers(req GetUserRequest) (*Response[[]User], error)
	GetUser(req GetUserRequest) (*User, error)
	CreateUser(user User) error
	DeleteUser(id string) error
	PutUser(user User) error
	PatchUser(user User) error

	GetPermissions(req GetPermissionRequest) (*Response[[]Permission], error)
	GetPermission(name string) (*Permission, error)
	CreatePermission(permission Permission) error
	DeletePermission(name string) error
	PutPermission(permission Permission) error

	GetRoles(req GetRoleRequest) (*Response[[]Role], error)
	GetRole(name string) (*Role, error)
	CreateRole(role Role) error
	PutRole(role Role) error
	DeleteRole(name string) error

	Check(req CheckRequest) (*CheckResponse, error)

	GetLMaps(req GetLMapRequest) (*Response[[]LMap], error)
	GetLMap(name string) (*LMap, error)
	CreateLMap(lmap LMap) error
	PutLMap(lmap LMap) error
	DeleteLMap(name string) error
}
