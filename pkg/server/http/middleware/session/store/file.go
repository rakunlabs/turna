package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/rakunlabs/ada/utils/securecookie"
	"github.com/rakunlabs/ada/utils/sessions"
)

type File struct {
	// SessionKey for file store.
	SessionKey string `cfg:"session_key"`
	Path       string `cfg:"path"`
}

type FileStore struct {
	path      string
	maxLength int
	codec     *securecookie.Codec
	options   sessions.Options
	initErr   error

	mu sync.RWMutex
}

func (f File) Store(opts sessions.Options) *FileStore {
	sessionKey := []byte(f.SessionKey)
	if len(sessionKey) == 0 {
		sessionKey = securecookie.GenerateRandomKey(32)
	}

	storePath := f.Path
	if storePath == "" {
		storePath = os.TempDir()
	}

	codec := securecookie.New(sessionKey, nil)
	codec.SetMaxAge(opts.MaxAge)

	return &FileStore{
		path:      storePath,
		maxLength: 1 << 20,
		codec:     codec,
		options:   opts,
		initErr:   os.MkdirAll(storePath, 0o700),
	}
}

func (s *FileStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return s.New(r, name)
}

func (s *FileStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := newSession(s, name, s.options)
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return session, nil
	}

	var sessionID string
	if err := s.codec.Decode(name, cookie.Value, &sessionID); err != nil {
		return session, err
	}

	values, err := s.load(sessionID)
	if err != nil {
		return session, err
	}

	session.ID = sessionID
	session.Values = values
	session.IsNew = false

	return session, nil
}

func (s *FileStore) Save(_ *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if session.Options.MaxAge < 0 {
		if session.ID != "" {
			s.delete(session.ID)
		}

		setSessionCookie(w, session.Name(), "", session.Options)

		return nil
	}

	if s.initErr != nil {
		return s.initErr
	}

	if session.ID == "" {
		sessionID, err := generateRandomKey()
		if err != nil {
			return fmt.Errorf("failed to generate session ID: %w", err)
		}
		session.ID = sessionID
	}

	if err := s.save(session.ID, session.Values); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	cookieValue, err := s.codec.Encode(session.Name(), session.ID)
	if err != nil {
		return err
	}

	setSessionCookie(w, session.Name(), cookieValue, session.Options)

	return nil
}

func (s *FileStore) filePath(sessionID string) string {
	return filepath.Join(s.path, "session_"+sessionID+".json")
}

func (s *FileStore) load(sessionID string) (map[string]any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.filePath(sessionID))
	if err != nil {
		return nil, err
	}

	if s.maxLength > 0 && len(data) > s.maxLength {
		return nil, fmt.Errorf("session data exceeds maximum length")
	}

	values := make(map[string]any)
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}

	return values, nil
}

func (s *FileStore) save(sessionID string, values map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(values)
	if err != nil {
		return err
	}

	if s.maxLength > 0 && len(data) > s.maxLength {
		return fmt.Errorf("session data exceeds maximum length")
	}

	return os.WriteFile(s.filePath(sessionID), data, 0o600)
}

func (s *FileStore) delete(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = os.Remove(s.filePath(sessionID))
}
