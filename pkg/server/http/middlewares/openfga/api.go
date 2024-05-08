package openfga

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"

	"github.com/oklog/ulid/v2"
	"github.com/openfga/language/pkg/go/transformer"
)

func (m *OpenFGA) Proxy(c echo.Context, trimPath string) error {
	path := "/" + strings.TrimPrefix(c.Request().URL.Path, trimPath)
	c.Request().URL.Path = path

	if m.SharedKey != "" {
		c.Request().Header.Set("Authorization", "Bearer "+m.SharedKey)
	}

	return m.openFGAProxy(c)
}

func (m *OpenFGA) Internal(c echo.Context, trimPath string) error {
	path := strings.TrimPrefix(c.Request().URL.Path, trimPath)
	if path == "user" {
		switch c.Request().Method {
		case http.MethodPost:
			return m.CreateUser(c)
		case http.MethodDelete:
			return m.DeleteUser(c)
		case http.MethodGet:
			return m.GetUser(c)
		case http.MethodPatch:
			return m.PatchUser(c)
		case http.MethodPut:
			return m.PutUser(c)
		default:
			return c.JSON(http.StatusMethodNotAllowed, ErrorResponse{
				Error: "Method not allowed",
			})
		}
	}

	if path == "users" {
		switch c.Request().Method {
		case http.MethodGet:
			return m.GetUsers(c)
		default:
			return c.JSON(http.StatusMethodNotAllowed, ErrorResponse{
				Error: "Method not allowed",
			})
		}
	}

	if path == "convert/dsl2json" {
		switch c.Request().Method {
		case http.MethodPost:
			return m.DSL2JSON(c)
		default:
			return c.JSON(http.StatusMethodNotAllowed, ErrorResponse{
				Error: "Method not allowed",
			})
		}
	}

	if path == "convert/json2dsl" {
		switch c.Request().Method {
		case http.MethodPost:
			return m.JSON2DSL(c)
		default:
			return c.JSON(http.StatusMethodNotAllowed, ErrorResponse{
				Error: "Method not allowed",
			})
		}
	}

	return c.JSON(http.StatusNotImplemented, ErrorResponse{
		Error: "Not implemented",
	})
}

func getAliasQuery(alias []string) (string, error) {
	userAlias := ""
	count := 0
	for _, a := range alias {
		if len(strings.Split(a, " ")) > 1 {
			return "", fmt.Errorf("alias should be one word")
		}

		if count > 0 {
			userAlias += " OR "
		} else {
			count++
		}

		userAlias += fmt.Sprintf(`alias @> '"%s"'`, a)
	}

	return userAlias, nil
}

func aliasExist(ctx context.Context, db *sqlx.DB, alias []string) error {
	userAlias, err := getAliasQuery(alias)
	if err != nil {
		return err
	}

	var existID string
	if err := db.GetContext(ctx, &existID, fmt.Sprintf(`SELECT id FROM user_list WHERE %s`, userAlias)); err == nil {
		return fmt.Errorf("alias already exist")
	}

	return nil
}

func aliasExistWithID(ctx context.Context, db *sqlx.DB, id string, alias []string) error {
	userAlias, err := getAliasQuery(alias)
	if err != nil {
		return err
	}

	var existID string
	if err := db.GetContext(ctx, &existID, fmt.Sprintf(`SELECT id FROM user_list WHERE %s AND id != $1`, userAlias), id); err == nil {
		return fmt.Errorf("alias already exist")
	}

	return nil
}

func (m *OpenFGA) CreateUser(c echo.Context) error {
	ctx := c.Request().Context()
	var req CreateUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: err.Error(),
		})
	}

	// check if alias is not exist
	if err := aliasExist(ctx, m.db, req.Alias); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	id := ulid.Make().String()
	if _, err := m.db.Exec(`INSERT INTO user_list (id, alias) VALUES ($1, $2)`, id, req.Alias); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	return c.JSON(http.StatusCreated, CreateUserResponse{ID: id})
}

func (m *OpenFGA) PatchUser(c echo.Context) error {
	ctx := c.Request().Context()

	var req PatchUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if req.ID == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID is required"})
	}

	if len(req.Alias) == 0 {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Alias is required"})
	}

	// check if alias is not exist
	if err := aliasExist(ctx, m.db, req.Alias); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if _, err := m.db.Exec(`UPDATE user_list SET alias = alias || $1 WHERE id = $2`, req.Alias, req.ID); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (m *OpenFGA) PutUser(c echo.Context) error {
	ctx := c.Request().Context()

	var req PutUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if req.ID == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID is required"})
	}

	if len(req.Alias) == 0 {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "Alias is required"})
	}

	// check if alias is not exist
	if err := aliasExistWithID(ctx, m.db, req.ID, req.Alias); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if _, err := m.db.Exec(`UPDATE user_list SET alias = $1 WHERE id = $2`, req.Alias, req.ID); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (m *OpenFGA) DeleteUser(c echo.Context) error {
	var req DeleteUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if req.ID == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID is required"})
	}

	if _, err := m.db.ExecContext(c.Request().Context(), `DELETE FROM user_list WHERE id = $1`, req.ID); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	return c.NoContent(http.StatusNoContent)
}

func (m *OpenFGA) GetUser(c echo.Context) error {
	var req GetUserRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	if len(req.Alias) == 0 && req.ID == "" {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: "ID or alias is required"})
	}

	var user User
	if req.ID != "" {
		query := `SELECT id, alias FROM user_list WHERE id = $1`
		if err := m.db.GetContext(c.Request().Context(), &user, query, req.ID); err != nil {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "User not found"})
		}
	} else if len(req.Alias) != 0 {
		alias, err := getAliasQuery([]string{req.Alias})
		if err != nil {
			return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		}
		query := fmt.Sprintf(`SELECT id, alias FROM user_list WHERE %s`, alias)
		if err := m.db.GetContext(c.Request().Context(), &user, query); err != nil {
			return c.JSON(http.StatusNotFound, ErrorResponse{Error: "User not found"})
		}
	}

	return c.JSON(http.StatusOK, user)
}

func (m *OpenFGA) GetUsers(c echo.Context) error {
	var users []User
	if err := m.db.SelectContext(c.Request().Context(), &users, `SELECT id, alias FROM user_list`); err != nil {
		return c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	if len(users) == 0 {
		return c.NoContent(http.StatusNoContent)
	}

	return c.JSON(http.StatusOK, users)
}

func (m *OpenFGA) DSL2JSON(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	jsonResponse, err := transformer.TransformDSLToJSON(string(body))
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	return c.JSONBlob(http.StatusOK, []byte(jsonResponse))
}

func (m *OpenFGA) JSON2DSL(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	dslResponse, err := transformer.TransformJSONStringToDSL(string(body))
	if err != nil {
		return c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
	}

	return c.Blob(http.StatusOK, echo.MIMETextPlain, []byte(*dslResponse))
}
