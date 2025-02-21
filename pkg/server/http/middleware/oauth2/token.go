package oauth2

import (
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/worldline-go/turna/pkg/server/http/httputil"
	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
	"golang.org/x/crypto/bcrypt"
)

type Token struct {
	KID  string `cfg:"kid"`
	Cert Cert   `cfg:"cert"`
}

type Cert struct {
	RSA RSAKey `cfg:"rsa"`
}

type RSAKey struct {
	PrivateKey       string `cfg:"private_key"`
	PrivateKeyBase64 string `cfg:"private_key_base64"`
	PublicKey        string `cfg:"public_key"`
	PublicKeyBase64  string `cfg:"public_key_base64"`

	private *rsa.PrivateKey `cfg:"-"`
	public  *rsa.PublicKey  `cfg:"-"`
}

func (k *RSAKey) GetPrivateKey() (string, error) {
	if k.PrivateKey != "" {
		return k.PrivateKey, nil
	}

	if k.PrivateKeyBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(k.PrivateKeyBase64)
		if err != nil {
			return "", fmt.Errorf("failed to decode private key base64: %w", err)
		}

		return string(b), nil
	}

	return "", nil
}

func (k *RSAKey) GetPublicKey() (string, error) {
	if k.PublicKey != "" {
		return k.PublicKey, nil
	}

	if k.PublicKeyBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(k.PublicKeyBase64)
		if err != nil {
			return "", fmt.Errorf("failed to decode public key base64: %w", err)
		}

		return string(b), nil
	}

	return "", nil
}

func CompareBcrypt(hash, password string) error {
	hashBytes, err := base64.StdEncoding.DecodeString(hash)
	if err != nil {
		return err
	}

	passwordBytes := []byte(password)

	return bcrypt.CompareHashAndPassword(hashBytes, passwordBytes)
}

func (m *Oauth2) GenerateToken(w http.ResponseWriter, userID string, user *data.UserExtended, clientID string, scope []string, defScope []string) {
	if user == nil {
		// get user from iam
		var err error
		user, err = m.iam.DB().GetUser(data.GetUserRequest{
			ID:            userID,
			AddScopeRoles: true,
		})
		if err != nil {
			httputil.HandleError(w, AccessTokenErrorResponse{
				Error:            "invalid_request",
				ErrorDescription: "user not found",
				code:             http.StatusBadRequest,
			})

			return
		}
	}

	// create access token
	claimsAccess := map[string]interface{}{
		"sub":                user.ID,
		"azp":                clientID,
		"name":               user.Details["name"],
		"preferred_username": user.Details["name"],
	}

	if v, ok := user.Details["email"]; ok {
		claimsAccess["email"] = v
	}

	if v, ok := user.Details["uid"]; ok {
		claimsAccess["preferred_username"] = v
	}

	// //////////////////////////////////////////
	// scope
	scopeMap := make(map[string]struct{})
	for _, s := range defScope {
		scopeMap[s] = struct{}{}
	}
	for _, s := range scope {
		scopeMap[s] = struct{}{}
	}

	roles := make(map[string]struct{})
	scopeList := make([]string, 0, len(scopeMap))
	for s := range scopeMap {
		if scopeRoles, ok := user.Scope[s]; ok {
			for _, r := range scopeRoles {
				roles[r] = struct{}{}
			}
		}

		scopeList = append(scopeList, s)
	}

	if len(scopeList) > 0 {
		claimsAccess["scope"] = strings.Join(scopeList, " ")
	}

	rolesList := make([]string, 0, len(roles))
	for r := range roles {
		rolesList = append(rolesList, r)
	}

	// //////////////////////////////////////////

	claimsAccess["resource_access"] = map[string]interface{}{
		"finops": map[string]interface{}{
			"roles": rolesList,
		},
	}

	// //////////////////////////////////////////
	accessToken, err := m.jwt.Generate(claimsAccess, time.Now().Add(time.Minute*15).Unix())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// generate refresh token
	claimsRefresh := map[string]interface{}{
		"sub": user.ID,
		"azp": clientID,
		"typ": "Refresh",
	}
	refreshToken, err := m.jwt.Generate(claimsRefresh, time.Now().Add(time.Hour*24).Unix())
	if err != nil {
		httputil.HandleError(w, AccessTokenErrorResponse{
			Error:            "server_error",
			ErrorDescription: err.Error(),
			code:             http.StatusInternalServerError,
		})

		return
	}

	// create response
	response := AccessTokenResponse{
		TokenType:             "Bearer",
		AccessToken:           accessToken,
		ExpiresIn:             900,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresIn: 86400,
		Scope:                 strings.Join(scopeList, " "),
	}

	// //////////////////////////////////////////
	// set response headers
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	httputil.JSON(w, http.StatusOK, response)
}
