package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

var errPrecondition = errors.New("record changed since it was read; reload it and review your draft before saving")

type writeCondition struct {
	revision   string
	createOnly bool
}
type writeConditionKey struct{}

var revisionTag = regexp.MustCompile(`^"(?:rev|keyrev)-[0-9]+"$`)

// Existing API clients may omit conditions. Management UI writes always send
// the version of the actual draft, not the newest background read.
func conditionalRequest(r *http.Request) (*http.Request, error) {
	match, none := r.Header.Get("If-Match"), r.Header.Get("If-None-Match")
	if match == "" && none == "" {
		return r, nil
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return r, nil
	}
	condition := writeCondition{}
	switch {
	case match != "" && none != "":
		return nil, errors.New("send either If-Match or If-None-Match, not both")
	case none == "*":
		condition.createOnly = true
	case revisionTag.MatchString(match):
		condition.revision = strings.Trim(match, `"`)
	default:
		return nil, errors.New(`expected If-Match: "rev-<number>" (or "keyrev-<number>" for API keys), or If-None-Match: *`)
	}
	return r.WithContext(context.WithValue(r.Context(), writeConditionKey{}, condition)), nil
}

// checkWriteCondition runs inside the same transaction as the write. Row locks
// serialize competing writers, and an advisory lock covers absent-row creates.
// Table/column names come only from fixed application constants.
func checkWriteCondition(ctx context.Context, tx *sql.Tx, table, key, id string) error {
	condition, ok := ctx.Value(writeConditionKey{}).(writeCondition)
	if !ok {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, table+":"+id); err != nil {
		return err
	}
	var revision string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT revision FROM %s WHERE %s = $1 FOR UPDATE`, table, key), id).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	exists := err == nil
	if condition.createOnly && !exists {
		return nil
	}
	if !condition.createOnly && exists && revision == condition.revision {
		return nil
	}
	return errPrecondition
}

func writeError(w http.ResponseWriter, err error, message string) {
	if errors.Is(err, errPrecondition) {
		httputil.HandleError(w, httputil.NewError(message, err, http.StatusPreconditionFailed))
		return
	}
	httputil.HandleError(w, httputil.NewError(message, err, http.StatusInternalServerError))
}

func setRecordETag(w http.ResponseWriter, revision string) {
	if revision != "" {
		w.Header().Set("ETag", `"`+revision+`"`)
	}
	w.Header().Set("Cache-Control", "no-store")
}
