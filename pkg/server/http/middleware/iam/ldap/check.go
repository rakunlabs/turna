package ldap

import (
	"errors"
	"fmt"

	"github.com/go-ldap/ldap/v3"
	"github.com/rakunlabs/into"
)

var ErrExceedPasswordRetryLimit = errors.New("exceed password retry limit")

func (l *Ldap) CheckPassword(username, password string) (bool, error) {
	l.mUser.Lock()
	defer l.mUser.Unlock()

	if l.connUser == nil || l.connUser.IsClosing() {
		conn, err := ldap.DialURL(l.Addr)
		if err != nil {
			return false, fmt.Errorf("failed connecting to LDAP server: %w", err)
		}

		into.ShutdownAdd(conn.Close, "ldap-user")

		l.connUser = conn
	}

	if _, err := l.connUser.SimpleBind(&ldap.SimpleBindRequest{
		Username: fmt.Sprintf("uid=%s,%s", username, l.UserBaseDN),
		Password: password,
	}); err != nil {
		switch {
		case ldap.IsErrorWithCode(err, ldap.LDAPResultConstraintViolation):
			return false, ErrExceedPasswordRetryLimit
		case ldap.IsErrorWithCode(err, ldap.LDAPResultInvalidCredentials):
			return false, nil
		case ldap.IsErrorWithCode(err, ldap.LDAPResultInsufficientAccessRights):
			return false, fmt.Errorf("insufficient access rights: %w", err)
		default:
			return false, fmt.Errorf("bind error: %w", err)
		}
	}

	return true, nil
}
