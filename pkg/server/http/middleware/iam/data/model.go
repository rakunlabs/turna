package data

import (
	"errors"
)

var (
	ErrConflict       = errors.New("conflict")
	ErrNotFound       = errors.New("not found")
	ErrInvalidRequest = errors.New("invalid request")

	True  = true
	False = false
)

// Token is a struct that represents a token table in the database.
type Token struct {
	ID          string `json:"id"          badgerhold:"unique"`
	Name        string `json:"name"        badgerhold:"index"`
	Token       string `json:"token"       badgerhold:"index"`
	Description string `json:"description"`
	ExpiresAt   string `json:"expires_at"`

	PermissionIDs []string `json:"permission_ids"`
	RoleIDs       []string `json:"role_ids"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// Permission is a struct that represents a permission table in the database.
type Permission struct {
	ID          string                 `json:"id"          badgerhold:"unique"`
	Name        string                 `json:"name"        badgerhold:"index"`
	Resources   []Resource             `json:"resources"`
	Description string                 `json:"description"`
	Data        map[string]interface{} `json:"data"`
	Scope       map[string][]string    `json:"scope"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

type PermissionExtended struct {
	*Permission

	Roles []IDName `json:"roles,omitempty"`
}

type PermissionPatch struct {
	Name        *string                `json:"name"`
	Resources   *[]Resource            `json:"resources"`
	Description *string                `json:"description"`
	Data        map[string]interface{} `json:"data"`
	Scope       map[string][]string    `json:"scope"`
}

type Resource struct {
	Hosts   []string `json:"hosts"`
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

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
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
	MixRoleIDs     []string               `json:"-"`
	Details        map[string]interface{} `json:"details"`
	Disabled       bool                   `json:"-"`
	ServiceAccount bool                   `json:"service_account"`
	Local          bool                   `json:"local"`
	PermissionIDs  []string               `json:"permission_ids"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

type UserCreate struct {
	User

	IsActive bool `json:"is_active"`
}

type UserPatch struct {
	Alias         *[]string               `json:"alias"`
	RoleIDs       *[]string               `json:"role_ids"`
	SyncRoleIDs   *[]string               `json:"sync_role_ids"`
	PermissionIDs *[]string               `json:"permission_ids"`
	Details       *map[string]interface{} `json:"details"`
	IsActive      *bool                   `json:"is_active"`
}

type UserExtended struct {
	*User

	IsActive    bool                `json:"is_active"`
	Roles       []IDName            `json:"roles,omitempty"`
	Permissions []IDName            `json:"permissions,omitempty"`
	Data        []interface{}       `json:"data,omitempty"`
	Scope       map[string][]string `json:"scope,omitempty"`
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

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

type LMapPatch struct {
	RoleIDs *[]string `json:"role_ids"`
}

type LMapCheckCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Dashboard struct {
	Roles []RoleExtended `json:"roles"`

	TotalRoles           uint64 `json:"total_roles"`
	TotalPermissions     uint64 `json:"total_permissions"`
	TotalUsers           uint64 `json:"total_users"`
	TotalServiceAccounts uint64 `json:"total_service_accounts"`
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

	Path        string   `json:"path"`
	Method      string   `json:"method"`
	Permissions []string `json:"permissions"`

	ServiceAccount *bool `json:"service_account"`
	Disabled       *bool `json:"disabled"`
	LocalUser      *bool `json:"local_user"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`

	AddRoles       bool `json:"add_role"`
	AddPermissions bool `json:"add_permissions"`
	AddData        bool `json:"add_data"`
	AddScopeRoles  bool `json:"add_scope_roles"`

	Sanitize bool `json:"sanitize"`
}

type GetPermissionRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	Path        string `json:"path"`
	Method      string `json:"method"`
	Description string `json:"description"`

	Limit  int64 `json:"limit"`
	Offset int64 `json:"offset"`

	Data map[string]string `json:"data"`

	AddRoles bool `json:"add_roles"`
}

type GetRoleRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`

	PermissionIDs []string `json:"permission_ids"`
	RoleIDs       []string `json:"role_ids"`

	Path        string   `json:"path"`
	Method      string   `json:"method"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`

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

	Host   string `json:"host"`
	Path   string `json:"path"`
	Method string `json:"method"`
}

type CheckRequestUser struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Method string `json:"method"`
}

type CheckResponse struct {
	Allowed bool `json:"allowed"`
}

type NameRequest struct {
	Name string `json:"name"`
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

type CheckConfig struct {
	// DefaultHosts for checkHost function. If resource hosts is empty, it will use this host.
	DefaultHosts []string `cfg:"default_hosts"`
	// NoHostCheck disable host checking on permission hosts.
	NoHostCheck bool `cfg:"no_host_check"`
}
