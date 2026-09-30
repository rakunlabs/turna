package sshserver

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

func readPrivateKey(path string) (ssh.Signer, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(b)
}

func loadAuthorizedKeys(inline, files []string) ([]ssh.PublicKey, error) {
	keys := make([]ssh.PublicKey, 0, len(inline)+len(files))
	for _, value := range inline {
		key, _, options, _, err := ssh.ParseAuthorizedKey([]byte(value))
		if err != nil {
			return nil, fmt.Errorf("invalid authorized key: %w", err)
		}
		if len(options) > 0 {
			return nil, fmt.Errorf("authorized key options are not supported: %v", options)
		}
		keys = append(keys, key)
	}
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("authorized key file %q: %w", path, err)
		}
		for len(b) > 0 {
			key, _, options, rest, err := ssh.ParseAuthorizedKey(b)
			if err != nil {
				return nil, fmt.Errorf("authorized key file %q: %w", path, err)
			}
			if len(options) > 0 {
				return nil, fmt.Errorf("authorized key file %q: key options are not supported: %v", path, options)
			}
			keys = append(keys, key)
			b = rest
		}
	}
	return keys, nil
}
