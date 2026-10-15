package access

import (
	"encoding/base64"

	"golang.org/x/crypto/bcrypt"
)

func ToBcrypt(password []byte) (string, error) {
	hash, err := bcrypt.GenerateFromPassword(password, bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(hash), nil
}
