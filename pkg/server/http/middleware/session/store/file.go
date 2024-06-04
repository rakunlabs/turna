package store

import (
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

type File struct {
	// SessionKey for file store.
	SessionKey string `cfg:"session_key"`
	Path       string `cfg:"path"`
}

func (f File) Store(opts sessions.Options) *sessions.FilesystemStore {
	sessionKey := []byte(f.SessionKey)
	if len(sessionKey) == 0 {
		sessionKey = securecookie.GenerateRandomKey(32)
	}

	fStore := sessions.NewFilesystemStore(f.Path, sessionKey)
	fStore.Options = &opts

	fStore.MaxLength(1 << 20)

	return fStore
}
