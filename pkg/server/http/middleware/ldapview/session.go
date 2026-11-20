package ldapview

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// session is one logged in user. The LDAP connection stays bound with the
// user's credentials, so the password is never kept.
type session struct {
	id   string
	conn *ldap.Conn
	info sessionInfo
	// dial holds how the session connected, without credentials, so other
	// binds can be tested against the same server.
	dial     connectOptions
	template string
	lastUsed time.Time
}

type sessionInfo struct {
	Server   string `json:"server"`
	Addr     string `json:"addr"`
	Mode     string `json:"mode"`
	BindDN   string `json:"bind_dn,omitempty"`
	BaseDN   string `json:"base_dn,omitempty"`
	StartTLS bool   `json:"start_tls,omitempty"`
	// Insecure is set when certificate checks are skipped.
	Insecure bool `json:"insecure,omitempty"`
	// CustomCA is set when extra CA certificates are trusted.
	CustomCA bool      `json:"custom_ca,omitempty"`
	Since    time.Time `json:"since"`
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*session
	idle     time.Duration
	max      int
	lastGC   time.Time
}

func newSessionStore(idle time.Duration, maxSessions int) *sessionStore {
	return &sessionStore{sessions: map[string]*session{}, idle: idle, max: maxSessions}
}

func (s *sessionStore) add(conn *ldap.Conn, info sessionInfo, dial connectOptions, template string) *session {
	b := make([]byte, 32)
	_, _ = rand.Read(b)

	sess := &session{
		id:       base64.RawURLEncoding.EncodeToString(b),
		conn:     conn,
		info:     info,
		dial:     dial,
		template: template,
		lastUsed: time.Now(),
	}

	s.mu.Lock()
	s.gcLocked()

	if s.max > 0 && len(s.sessions) >= s.max {
		s.evictOldestLocked()
	}

	s.sessions[sess.id] = sess
	s.mu.Unlock()

	return sess
}

func (s *sessionStore) get(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.gcLocked()

	sess, ok := s.sessions[id]
	if !ok {
		return nil
	}

	if sess.conn.IsClosing() {
		delete(s.sessions, id)

		return nil
	}

	sess.lastUsed = time.Now()

	return sess
}

func (s *sessionStore) remove(id string) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	delete(s.sessions, id)
	s.mu.Unlock()

	if ok {
		_ = sess.conn.Close()
	}
}

// gcLocked closes idle sessions at most once a minute.
func (s *sessionStore) gcLocked() {
	now := time.Now()
	if now.Sub(s.lastGC) < time.Minute {
		return
	}

	s.lastGC = now

	for id, sess := range s.sessions {
		if now.Sub(sess.lastUsed) > s.idle || sess.conn.IsClosing() {
			delete(s.sessions, id)

			go sess.conn.Close()
		}
	}
}

// evictOldestLocked closes the least recently used session to make room.
func (s *sessionStore) evictOldestLocked() {
	var oldest *session
	for _, sess := range s.sessions {
		if oldest == nil || sess.lastUsed.Before(oldest.lastUsed) {
			oldest = sess
		}
	}

	if oldest != nil {
		delete(s.sessions, oldest.id)

		go oldest.conn.Close()
	}
}
