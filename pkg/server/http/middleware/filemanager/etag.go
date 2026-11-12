package filemanager

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

const (
	// etagWeak is W/"<modtime>-<size>": cheap, but browsers and Go refuse it for If-Range.
	etagWeak = "weak"
	// etagStrong is "<modtime>-<size>" without W/. Writes go through a rename, so the
	// modtime changes with every write and the tag is safe to use for byte ranges.
	etagStrong = "strong"
	// etagHash is a SHA-256 of the content. The first request after a change reads the whole file.
	etagHash = "hash"
)

// ETagRule picks the ETag mode for files matching Pattern.
type ETagRule struct {
	// Pattern is a doublestar glob on the path inside the directory, without
	// a leading slash, e.g. "videos/**" or "**/*.mp4".
	Pattern string `cfg:"pattern"`
	// ETag is "weak", "strong" or "hash".
	ETag string `cfg:"etag"`
}

type etagEntry struct {
	modtime int64
	size    int64
	tag     string
}

func validETagMode(mode string) error {
	switch mode {
	case etagWeak, etagStrong, etagHash:
		return nil
	default:
		return fmt.Errorf("unsupported etag mode %q, use %q, %q or %q", mode, etagWeak, etagStrong, etagHash)
	}
}

func (m *FileManager) initETag() error {
	if m.ETag == "" {
		m.ETag = etagWeak
	}

	if err := validETagMode(m.ETag); err != nil {
		return err
	}

	for i, r := range m.ETagRules {
		r.Pattern = strings.TrimPrefix(r.Pattern, "/")
		if r.Pattern == "" || !doublestar.ValidatePattern(r.Pattern) {
			return fmt.Errorf("etag_rules[%d]: invalid pattern %q", i, r.Pattern)
		}

		if err := validETagMode(r.ETag); err != nil {
			return fmt.Errorf("etag_rules[%d]: %w", i, err)
		}

		m.ETagRules[i] = r
	}

	return nil
}

// etagMode returns the mode for the file at name; the first matching rule wins.
func (m *FileManager) etagMode(name string) string {
	for _, r := range m.ETagRules {
		if doublestar.MatchUnvalidated(r.Pattern, name) {
			return r.ETag
		}
	}

	return m.ETag
}

// etag returns the validator of the file at name, or "" when it cannot be computed.
func (m *FileManager) etag(ctx context.Context, name string, info fs.FileInfo) string {
	modtime, size := info.ModTime().UnixNano(), info.Size()
	stamp := strconv.FormatInt(modtime, 36) + "-" + strconv.FormatInt(size, 36)

	switch m.etagMode(name) {
	case etagStrong:
		return `"` + stamp + `"`
	case etagHash:
	default:
		return `W/"` + stamp + `"`
	}

	// one entry per path, so edited files replace their old hash instead of piling up
	if v, ok := m.etagCache.Load(name); ok {
		if e := v.(etagEntry); e.modtime == modtime && e.size == size {
			return e.tag
		}
	}

	f, _, err := m.store.Open(ctx, name)
	if err != nil {
		slog.Warn("file_manager: cannot generate etag", "file", name, "err", err.Error())

		return ""
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		slog.Warn("file_manager: cannot generate etag", "file", name, "err", err.Error())

		return ""
	}

	tag := `"` + base64.RawURLEncoding.EncodeToString(h.Sum(nil)) + `"`
	m.etagCache.Store(name, etagEntry{modtime: modtime, size: size, tag: tag})

	return tag
}

// forgetETags drops cached hashes of name and everything below it.
func (m *FileManager) forgetETags(name string) {
	m.etagCache.Range(func(k, _ any) bool {
		if p := k.(string); p == name || strings.HasPrefix(p, name+"/") {
			m.etagCache.Delete(k)
		}

		return true
	})
}

// matchETag reports whether the If-Match or If-None-Match header value matches
// current. It uses weak comparison, so W/"x" and "x" are equal: the compress
// middleware weakens strong tags, and the UI echoes back whatever it received.
// An empty current means the file does not exist and only fails to match.
func matchETag(header, current string) bool {
	if current == "" {
		return false
	}

	current = strings.TrimPrefix(current, "W/")

	for t := range strings.SplitSeq(header, ",") {
		t = strings.TrimSpace(t)
		if t == "*" || strings.TrimPrefix(t, "W/") == current {
			return true
		}
	}

	return false
}

// checkPreconditions applies If-Match and If-None-Match to a write. current is
// the ETag of the existing file, "" when there is none. It writes a 412 and
// returns false when the request must not proceed.
func checkPreconditions(w http.ResponseWriter, r *http.Request, current string) bool {
	if im := r.Header.Get("If-Match"); im != "" && !matchETag(im, current) {
		msg := "file changed since it was opened"
		if current == "" {
			msg = "file no longer exists"
		}

		writeJSON(w, http.StatusPreconditionFailed, apiError{Error: msg})

		return false
	}

	if inm := r.Header.Get("If-None-Match"); inm != "" && matchETag(inm, current) {
		writeJSON(w, http.StatusPreconditionFailed, apiError{Error: "file already exists"})

		return false
	}

	return true
}
