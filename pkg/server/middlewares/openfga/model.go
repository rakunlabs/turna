package openfga

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

type ErrorResponse struct {
	Error string `json:"error,omitempty"`
}

type CreateUserRequest struct {
	Alias []string `json:"alias"`
}

type CreateUserResponse struct {
	ID string `json:"id"`
}

type PatchUserRequest struct {
	ID    string   `json:"id"`
	Alias []string `json:"alias"`
}

type PutUserRequest PatchUserRequest

type GetUserRequest struct {
	ID    string `json:"id"    query:"id"`
	Alias string `json:"alias" query:"alias"`
}

type DeleteUserRequest struct {
	ID string `json:"id" query:"id"`
}

type User struct {
	ID    string `json:"id"    db:"id"    query:"id"`
	Alias Alias  `json:"alias" db:"alias" query:"alias"`
}

type Alias []string

func (a *Alias) Scan(val interface{}) error {
	switch v := val.(type) {
	case []byte:
		json.Unmarshal(v, &a)
		return nil
	case string:
		json.Unmarshal([]byte(v), &a)
		return nil
	default:
		return fmt.Errorf("Unsupported type: %T", v)
	}
}

func (a *Alias) Value() (driver.Value, error) {
	return json.Marshal(a)
}
