package store

import (
	"github.com/gorilla/sessions"
)

type File struct {
	Path string `cfg:"path"`
}

func (f File) Store(sessionKey []byte, opts sessions.Options) *sessions.FilesystemStore {
	fStore := sessions.NewFilesystemStore(f.Path, sessionKey)
	fStore.Options = &opts

	fStore.MaxLength(1 << 20)

	return fStore
}
