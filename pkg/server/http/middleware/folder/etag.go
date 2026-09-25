package folder

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"time"
)

const (
	etagWeak   = "weak"
	etagStrong = "strong"
)

type etagCacheKey struct {
	path    string
	modtime int64
	size    int64
}

// etag returns the ETag of content and rewinds it to the beginning.
func (f *Folder) etag(filePath string, modtime time.Time, content io.ReadSeeker) (string, error) {
	size, err := content.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}

	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	if f.ETag == etagWeak {
		if modtime.IsZero() {
			// without modtime the size alone cannot identify the content
			return "", nil
		}

		return "W/\"" + strconv.FormatInt(modtime.UnixNano(), 36) + "-" + strconv.FormatInt(size, 36) + "\"", nil
	}

	// custom content may differ per request, never cache it
	cacheable := f.customContent == nil && filePath != ""
	key := etagCacheKey{path: filePath, modtime: modtime.UnixNano(), size: size}

	if cacheable {
		if v, ok := f.etagCache.Load(key); ok {
			return v.(string), nil
		}
	}

	h := sha256.New()
	if _, err := io.Copy(h, content); err != nil {
		return "", fmt.Errorf("hash content: %w", err)
	}

	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	etag := "\"" + base64.RawURLEncoding.EncodeToString(h.Sum(nil)) + "\""

	if cacheable {
		f.etagCache.Store(key, etag)
	}

	return etag, nil
}
