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
	ID          string                 `json:"id"          badgerhold:"unique"`
	Name        string                 `json:"name"        badgerhold:"index"`
	Resources   []Resource             `json:"resources"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`
}

type PermissionPatch struct {
	Name        *string                `json:"name"`
	Resources   *[]Resource            `json:"resources"`
	Description *string                `json:"description"`
	Data        map[string]interface{} `json:"data"`
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

type RolePatch struct {
	Name          *string                 `json:"name"`
	PermissionIDs *[]string               `json:"permission_ids"`
	RoleIDs       *[]string               `json:"role_ids"`
	Data          *map[string]interface{} `json:"data"`
	Description   *string                 `json:"description"`
}

type RoleRelation struct {
	Roles       *[]string `json:"roles"`
	Permissions *[]string `json:"permissions"`
}

type RoleExtended struct {
	*Role

	Roles       []IDName `json:"roles,omitempty"`
	Permissions []IDName `json:"permissions,omitempty"`
	TotalUsers  uint64   `json:"total_users"`
}

type PermissionIDs struct {
	PermissionIDs []string `json:"permission_ids"`
}

type RoleIDs struct {
	RoleIDs []string `json:"role_ids"`
}

type Alias struct {
	Name string `json:"name" badgerhold:"unique"`
	ID   string `json:"id"   badgerhold:"index"`
}

// User is a struct that represents a user table in the database.
type User struct {
	ID             string                 `json:"id"              badgerhold:"unique"`
	Alias          []string               `json:"alias"`
	RoleIDs        []string               `json:"role_ids"`
	SyncRoleIDs    []string               `json:"sync_role_ids"`
	Details        map[string]interface{} `json:"details"`
	Disabled       bool                   `json:"-"`
	ServiceAccount bool                   `json:"service_account"`
}

type UserCreate struct {
	User

	IsActive bool `json:"is_active"`
}

type UserPatch struct {
	Alias       *[]string               `json:"alias"`
	RoleIDs     *[]string               `json:"role_ids"`
	SyncRoleIDs *[]string               `json:"sync_role_ids"`
	Details     *map[string]interface{} `json:"details"`
	IsActive    *bool                   `json:"is_active"`
}

type UserExtended struct {
	*User

	IsActive    bool          `json:"is_active"`
	Roles       []IDName      `json:"roles,omitempty"`
	Permissions []IDName      `json:"permissions,omitempty"`
	Data        []interface{} `json:"data,omitempty"`
}

type IDName struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type UserInfo struct {
	Details     map[string]interface{} `json:"details"`
	Roles       []string               `json:"roles,omitempty"`
	Permissions []string               `json:"permissions,omitempty"`
	Data        []interface{}          `json:"data,omitempty"`
	IsActive    bool                   `json:"is_active"`
}

type LMap struct {
	Name    string   `json:"name"     badgerhold:"unique"`
	RoleIDs []string `json:"role_ids"`
}

type LMapPatch struct {
	RoleIDs *[]string `json:"role_ids"`
}

type LMapCheckCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// //////////////////////////////////////////////////////////////////////

type Response[T any] struct {
	Message *Message `json:"message,omitempty"`
	Meta    *Meta    `json:"meta,omitempty"`
	Payload T        `json:"payload"`
}

type ResponseVersion struct {
	Version uint64 `json:"version"`
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

type ResponseCreateBulk struct {
	IDs []string `json:"ids"`
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

	ServiceAccount *bool `json:"service_account"`
	Disabled       *bool `json:"disabled"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`

	AddRoles       bool `json:"add_role"`
	AddPermissions bool `json:"add_permissions"`
	AddData        bool `json:"add_data"`

	Sanitize bool `json:"sanitize"`
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

type CheckRequestUser struct {
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
	PatchUser(id string, user UserPatch) error

	GetPermissions(req GetPermissionRequest) (*Response[[]Permission], error)
	GetPermission(id string) (*Permission, error)
	CreatePermission(permission Permission) (string, error)
	CreatePermissions(permission []Permission) ([]string, error)
	DeletePermission(id string) error
	PutPermission(permission Permission) error
	PatchPermission(id string, permission PermissionPatch) error

	GetRoles(req GetRoleRequest) (*Response[[]RoleExtended], error)
	GetRole(req GetRoleRequest) (*RoleExtended, error)
	CreateRole(role Role) (string, error)
	PutRole(role Role) error
	DeleteRole(id string) error
	PatchRole(id string, role RolePatch) error
	PutRoleRelation(relation map[string]RoleRelation) error
	GetRoleRelation() (map[string]RoleRelation, error)

	Check(req CheckRequest) (*CheckResponse, error)

	GetLMaps(req GetLMapRequest) (*Response[[]LMap], error)
	GetLMap(name string) (*LMap, error)
	CreateLMap(lmap LMap) error
	PutLMap(lmap LMap) error
	DeleteLMap(name string) error

	LMapRoleIDs() LMapRoleIDs
	CheckCreateLMap(groups []LMapCheckCreate)

	Backup(w io.Writer, since uint64) (uint64, error)
	Restore(r io.Reader) error
	Version() uint64
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
