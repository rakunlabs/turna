//go:build swagger

package auth

// The IAM package is included because the auth API exposes the same IAM
// resources and response models under its own prefix.
//
//go:generate go tool swag init -pd -d ./,../iam -g swagger.go --ot go,json -o ./docs

// @title Turna Auth API
// @version 1.0
// @description PostgreSQL-backed identity provider API for IAM, OAuth 2.0, OpenID Connect, passkeys, API keys, LDAP and SAML.
// @BasePath /auth
// @accept json
// @produce json
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description OAuth 2.0 bearer token. Use: Bearer {token}
// @securityDefinitions.apikey XUserAuth
// @in header
// @name X-User
// @description Authenticated user forwarded by the session middleware.
func swaggerInfo() {}

type swaggerObject map[string]interface{}

// @Summary List auth settings
// @Tags settings
// @Security XUserAuth
// @Param namespace query string false "Filter by namespace"
// @Param limit query int false "Maximum results"
// @Param offset query int false "Results to skip"
// @Success 200 {object} Response[[]SettingMeta]
// @Failure 400,500 {object} httputil.Error
// @Router /v1/settings [get]
func swaggerListSettings() {}

// @Summary Get an auth setting
// @Tags settings
// @Security XUserAuth
// @Param namespace path string true "Setting namespace"
// @Success 200 {object} Response[Setting]
// @Failure 404,500 {object} httputil.Error
// @Router /v1/settings/{namespace} [get]
func swaggerGetSetting() {}

// @Summary Save an auth setting
// @Description Replaces a runtime setting and reloads the auth snapshot.
// @Tags settings
// @Security XUserAuth
// @Param namespace path string true "Setting namespace"
// @Param setting body SettingRequest true "Setting value"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,500 {object} httputil.Error
// @Router /v1/settings/{namespace} [put]
func swaggerPutSetting() {}

// @Summary List OAuth clients
// @Tags oauth-clients
// @Security XUserAuth
// @Success 200 {object} Response[[]ConfigMeta]
// @Failure 400,500 {object} httputil.Error
// @Router /v1/oauth/clients [get]
func swaggerListOAuthClients() {}

// @Summary Get an OAuth client
// @Tags oauth-clients
// @Security XUserAuth
// @Param id path string true "Client ID or metadata URL"
// @Success 200 {object} Response[ConfigResource]
// @Failure 404,500 {object} httputil.Error
// @Router /v1/oauth/clients/{id} [get]
func swaggerGetOAuthClient() {}

// @Summary Save an OAuth client
// @Tags oauth-clients
// @Security XUserAuth
// @Param id path string true "Client ID or metadata URL"
// @Param client body ConfigRequest true "OAuth client configuration"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,500 {object} httputil.Error
// @Router /v1/oauth/clients/{id} [put]
func swaggerPutOAuthClient() {}

// @Summary Delete an OAuth client
// @Tags oauth-clients
// @Security XUserAuth
// @Param id path string true "Client ID or metadata URL"
// @Success 200 {object} Response[swaggerObject]
// @Failure 404,500 {object} httputil.Error
// @Router /v1/oauth/clients/{id} [delete]
func swaggerDeleteOAuthClient() {}

// @Summary List OAuth providers
// @Tags oauth-providers
// @Security XUserAuth
// @Success 200 {object} Response[[]ConfigMeta]
// @Failure 400,500 {object} httputil.Error
// @Router /v1/oauth/providers [get]
func swaggerListOAuthProviders() {}

// @Summary Get an OAuth provider
// @Tags oauth-providers
// @Security XUserAuth
// @Param id path string true "Provider ID"
// @Success 200 {object} Response[ConfigResource]
// @Failure 404,500 {object} httputil.Error
// @Router /v1/oauth/providers/{id} [get]
func swaggerGetOAuthProvider() {}

// @Summary Save an OAuth provider
// @Tags oauth-providers
// @Security XUserAuth
// @Param id path string true "Provider ID"
// @Param provider body ConfigRequest true "OAuth provider configuration"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,500 {object} httputil.Error
// @Router /v1/oauth/providers/{id} [put]
func swaggerPutOAuthProvider() {}

// @Summary Delete an OAuth provider
// @Tags oauth-providers
// @Security XUserAuth
// @Param id path string true "Provider ID"
// @Success 200 {object} Response[swaggerObject]
// @Failure 404,500 {object} httputil.Error
// @Router /v1/oauth/providers/{id} [delete]
func swaggerDeleteOAuthProvider() {}

// @Summary Issue tokens
// @Description OAuth 2.0 token endpoint. Supports authorization_code, client_credentials, password, refresh_token, device_code, token exchange and email-code grants.
// @Tags oauth2
// @Accept application/x-www-form-urlencoded
// @Param grant_type formData string true "OAuth grant type"
// @Param client_id formData string false "Client ID; Basic authentication takes precedence"
// @Param client_secret formData string false "Client secret; Basic authentication takes precedence"
// @Param code formData string false "Authorization code"
// @Param redirect_uri formData string false "Authorization redirect URI"
// @Param code_verifier formData string false "PKCE verifier"
// @Param refresh_token formData string false "Refresh token"
// @Param username formData string false "Username for password grant"
// @Param password formData string false "Password for password grant"
// @Param totp formData string false "TOTP or recovery code"
// @Param scope formData string false "Space-separated scopes"
// @Param resource formData string false "RFC 8707 resource indicator"
// @Success 200 {object} AccessTokenResponse
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/token [post]
func swaggerToken() {}

// @Summary Start local authorization
// @Description Starts the OAuth 2.0 authorization-code flow and redirects to consent.
// @Tags oauth2
// @Param response_type query string true "Must be code" Enums(code)
// @Param client_id query string true "Client ID"
// @Param redirect_uri query string true "Registered redirect URI"
// @Param scope query string false "Space-separated scopes"
// @Param state query string false "Client state"
// @Param nonce query string false "OIDC nonce"
// @Param code_challenge query string false "PKCE challenge"
// @Param code_challenge_method query string false "PKCE method" Enums(S256,plain)
// @Param resource query []string false "RFC 8707 resource indicators" collectionFormat(multi)
// @Success 302
// @Failure 400,500 {object} AccessTokenErrorResponse
// @Router /oauth2/authorize [get]
func swaggerAuthorize() {}

// @Summary Start upstream OAuth login
// @Tags oauth2
// @Param provider path string true "Provider ID"
// @Param client_id query string true "Client ID"
// @Param redirect_uri query string true "Registered redirect URI"
// @Param scope query string false "Space-separated scopes"
// @Param state query string false "Client state"
// @Success 307
// @Failure 400,404,500 {object} AccessTokenErrorResponse
// @Router /oauth2/auth/{provider} [get]
func swaggerProviderAuthorize() {}

// @Summary Handle upstream OAuth callback
// @Tags oauth2
// @Param provider path string true "Provider ID"
// @Param code query string true "Provider authorization code"
// @Param state query string true "Provider state"
// @Success 307
// @Failure 400,404,500 {object} AccessTokenErrorResponse
// @Router /oauth2/code/{provider} [get]
func swaggerProviderCallback() {}

// @Summary Get JSON Web Key Set
// @Tags oidc
// @Success 200 {object} JWKSResponse
// @Failure 500 {object} AccessTokenErrorResponse
// @Router /oauth2/certs [get]
func swaggerCerts() {}

// @Summary Get OpenID Connect user claims
// @Tags oidc
// @Security BearerAuth
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/userinfo [get]
func swaggerUserInfo() {}

// @Summary Get customized OpenID Connect user claims
// @Tags oidc
// @Security BearerAuth
// @Param custom path string true "Custom info set"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/userinfo/{custom} [get]
func swaggerCustomUserInfo() {}

// @Summary Get OpenID Connect discovery metadata
// @Tags oidc
// @Success 200 {object} swaggerObject
// @Router /oauth2/.well-known/openid-configuration [get]
func swaggerOpenIDConfiguration() {}

// @Summary Get OAuth authorization server metadata
// @Tags oauth2
// @Success 200 {object} swaggerObject
// @Router /oauth2/.well-known/oauth-authorization-server [get]
func swaggerAuthorizationServerMetadata() {}

// @Summary Revoke a token
// @Tags oauth2
// @Accept application/x-www-form-urlencoded
// @Param token formData string true "Access or refresh token"
// @Param token_type_hint formData string false "Token type hint"
// @Success 200
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/revoke [post]
func swaggerRevoke() {}

// @Summary Introspect a token
// @Tags oauth2
// @Accept application/x-www-form-urlencoded
// @Param token formData string true "Access or refresh token"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/introspect [post]
func swaggerIntrospect() {}

// @Summary Start device authorization
// @Tags device-flow
// @Accept application/x-www-form-urlencoded
// @Param client_id formData string true "Client ID"
// @Param scope formData string false "Space-separated scopes"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/device_authorization [post]
func swaggerDeviceAuthorization() {}

// @Summary Get a pending device request
// @Tags device-flow
// @Security XUserAuth
// @Param user_code path string true "Device user code"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/device/{user_code} [get]
func swaggerDeviceInfo() {}

// @Summary Approve or deny a device request
// @Tags device-flow
// @Security XUserAuth
// @Param decision body swaggerObject true "User code and decision"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,404,500 {object} httputil.Error
// @Router /v1/device [post]
func swaggerDeviceApprove() {}

// @Summary Begin or finish passkey registration
// @Tags passkeys
// @Security XUserAuth
// @Param request body PasskeyRegisterRequest true "Registration request; omit credential to begin"
// @Success 200 {object} Response[PasskeyBeginResponse]
// @Failure 400,401,503,500 {object} httputil.Error
// @Router /v1/passkey/register [post]
func swaggerPasskeyRegister() {}

// @Summary List passkey credentials
// @Tags passkeys
// @Security XUserAuth
// @Success 200 {object} Response[[]swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/passkey/credentials [get]
func swaggerPasskeyCredentials() {}

// @Summary Delete a passkey credential
// @Tags passkeys
// @Security XUserAuth
// @Param id path string true "Credential ID"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/passkey/credentials/{id} [delete]
func swaggerPasskeyDelete() {}

// @Summary Get current account
// @Tags self-service
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/me [get]
func swaggerMe() {}

// @Summary Change current account password
// @Tags self-service
// @Security XUserAuth
// @Param request body map[string]string true "Current and new password"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/me/password [post]
func swaggerMePassword() {}

// @Summary Get TOTP status
// @Tags totp
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/totp [get]
func swaggerTOTPStatus() {}

// @Summary Begin TOTP registration
// @Tags totp
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/totp/register [post]
func swaggerTOTPRegister() {}

// @Summary Confirm TOTP registration
// @Tags totp
// @Security XUserAuth
// @Param request body map[string]string true "TOTP code"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/totp/confirm [post]
func swaggerTOTPConfirm() {}

// @Summary Generate TOTP recovery codes
// @Tags totp
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/totp/recovery [post]
func swaggerTOTPRecovery() {}

// @Summary Disable TOTP
// @Tags totp
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/totp [delete]
func swaggerTOTPDelete() {}

// @Summary Rotate JWT signing key
// @Tags maintenance
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/jwt/rotate [post]
func swaggerRotateJWT() {}

// @Summary Rotate record encryption key
// @Tags maintenance
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/encryption/rotate [post]
func swaggerRotateEncryption() {}

// @Summary Purge expired flow codes
// @Tags maintenance
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/maintenance/flow-codes/purge [post]
func swaggerPurgeFlowCodes() {}

// @Summary Get auth capabilities
// @Tags system
// @Success 200 {object} swaggerObject
// @Router /v1/capabilities [get]
func swaggerCapabilities() {}

// @Summary Delete an auth setting
// @Tags settings
// @Security XUserAuth
// @Param namespace path string true "Setting namespace"
// @Success 200 {object} Response[swaggerObject]
// @Failure 404,500 {object} httputil.Error
// @Router /v1/settings/{namespace} [delete]
func swaggerDeleteSetting() {}

// @Summary List session provider groups
// @Tags session-providers
// @Security XUserAuth
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/session-providers [get]
func swaggerSessionProviders() {}

// @Summary Get a session provider group
// @Tags session-providers
// @Security XUserAuth
// @Param group path string true "Provider group"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/session-providers/{group} [get]
func swaggerSessionProviderGroup() {}

// @Summary List LDAP configurations
// @Tags ldap-config
// @Security XUserAuth
// @Success 200 {object} Response[[]ConfigMeta]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/ldap/configs [get]
func swaggerListLDAPConfigs() {}

// @Summary Get an LDAP configuration
// @Tags ldap-config
// @Security XUserAuth
// @Param id path string true "LDAP configuration ID"
// @Success 200 {object} Response[ConfigResource]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/ldap/configs/{id} [get]
func swaggerGetLDAPConfig() {}

// @Summary Save an LDAP configuration
// @Tags ldap-config
// @Security XUserAuth
// @Param id path string true "LDAP configuration ID"
// @Param config body ConfigRequest true "LDAP configuration"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/ldap/configs/{id} [put]
func swaggerPutLDAPConfig() {}

// @Summary Delete an LDAP configuration
// @Tags ldap-config
// @Security XUserAuth
// @Param id path string true "LDAP configuration ID"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/ldap/configs/{id} [delete]
func swaggerDeleteLDAPConfig() {}

// @Summary List API keys
// @Tags api-keys
// @Security XUserAuth
// @Success 200 {object} Response[[]swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/api-keys [get]
func swaggerListAPIKeys() {}

// @Summary Create an API key
// @Description Returns the API key secret once. Store it securely.
// @Tags api-keys
// @Security XUserAuth
// @Param request body swaggerObject true "API key configuration"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/api-keys [post]
func swaggerCreateAPIKey() {}

// @Summary Update an API key
// @Tags api-keys
// @Security XUserAuth
// @Param id path string true "API key ID"
// @Param request body swaggerObject true "API key changes"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,404,500 {object} httputil.Error
// @Router /v1/api-keys/{id} [patch]
func swaggerUpdateAPIKey() {}

// @Summary Delete an API key
// @Tags api-keys
// @Security XUserAuth
// @Param id path string true "API key ID"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/api-keys/{id} [delete]
func swaggerDeleteAPIKey() {}

// @Summary List API key principals
// @Tags api-key-principals
// @Security XUserAuth
// @Success 200 {object} Response[[]swaggerObject]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/api-key-principals [get]
func swaggerListAPIKeyPrincipals() {}

// @Summary Create an API key principal
// @Tags api-key-principals
// @Security XUserAuth
// @Param request body swaggerObject true "Principal and API key configuration"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/api-key-principals [post]
func swaggerCreateAPIKeyPrincipal() {}

// @Summary Update an API key principal
// @Tags api-key-principals
// @Security XUserAuth
// @Param id path string true "API key ID"
// @Param request body swaggerObject true "Principal and API key changes"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,404,500 {object} httputil.Error
// @Router /v1/api-key-principals/{id} [patch]
func swaggerUpdateAPIKeyPrincipal() {}

// @Summary Delete an API key principal
// @Tags api-key-principals
// @Security XUserAuth
// @Param id path string true "API key ID"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/api-key-principals/{id} [delete]
func swaggerDeleteAPIKeyPrincipal() {}

// @Summary Authenticate with an API key
// @Tags oauth2
// @Param X-API-Key header string false "API key; the raw Authorization header is also accepted"
// @Success 200 {object} AccessTokenResponse
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/api-key [get]
func swaggerAPIKeyGet() {}

// @Summary Authenticate with an API key
// @Tags oauth2
// @Param request body swaggerObject false "API key authentication request"
// @Success 200 {object} AccessTokenResponse
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/api-key [post]
func swaggerAPIKeyPost() {}

// @Summary Authenticate with a passkey
// @Tags passkeys
// @Param request body swaggerObject true "Passkey challenge or assertion"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/passkey [post]
func swaggerPasskeyToken() {}

// @Summary Request an email login code
// @Tags email-auth
// @Param request body swaggerObject true "Email code request"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/email [post]
func swaggerEmailCode() {}

// @Summary Sign up a local user
// @Tags signup
// @Param request body swaggerObject true "Signup details"
// @Success 200 {object} swaggerObject
// @Failure 400,409,500 {object} AccessTokenErrorResponse
// @Router /oauth2/signup [post]
func swaggerSignup() {}

// @Summary Verify a signup code
// @Tags signup
// @Param request body swaggerObject true "Signup verification"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/signup/verify [post]
func swaggerSignupVerify() {}

// @Summary Request a password reset
// @Tags password-reset
// @Param request body swaggerObject true "Password reset request"
// @Success 200 {object} swaggerObject
// @Failure 400,500 {object} AccessTokenErrorResponse
// @Router /oauth2/password-reset [post]
func swaggerPasswordReset() {}

// @Summary Confirm a password reset
// @Tags password-reset
// @Param request body swaggerObject true "Reset code and new password"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/password-reset/confirm [post]
func swaggerPasswordResetConfirm() {}

// @Summary Register an OAuth client
// @Description Implements RFC 7591 dynamic client registration.
// @Tags client-registration
// @Param request body swaggerObject true "Client metadata"
// @Success 201 {object} swaggerObject
// @Failure 400,401,500 {object} AccessTokenErrorResponse
// @Router /oauth2/register [post]
func swaggerRegisterClient() {}

// @Summary Get registered OAuth client metadata
// @Tags client-registration
// @Security BearerAuth
// @Param client_id path string true "Client ID"
// @Success 200 {object} swaggerObject
// @Failure 401,404,500 {object} AccessTokenErrorResponse
// @Router /oauth2/register/{client_id} [get]
func swaggerGetRegisteredClient() {}

// @Summary Update registered OAuth client metadata
// @Tags client-registration
// @Security BearerAuth
// @Param client_id path string true "Client ID"
// @Param request body swaggerObject true "Client metadata"
// @Success 200 {object} swaggerObject
// @Failure 400,401,404,500 {object} AccessTokenErrorResponse
// @Router /oauth2/register/{client_id} [put]
func swaggerUpdateRegisteredClient() {}

// @Summary Delete a registered OAuth client
// @Tags client-registration
// @Security BearerAuth
// @Param client_id path string true "Client ID"
// @Success 204
// @Failure 401,404,500 {object} AccessTokenErrorResponse
// @Router /oauth2/register/{client_id} [delete]
func swaggerDeleteRegisteredClient() {}

// @Summary Show OAuth consent
// @Tags oauth2
// @Security XUserAuth
// @Param flow query string true "Pending authorization flow ID"
// @Success 200 {string} string "Consent HTML"
// @Failure 401,404 {string} string
// @Router /oauth2/consent [get]
func swaggerConsent() {}

// @Summary Submit OAuth consent
// @Tags oauth2
// @Security XUserAuth
// @Accept application/x-www-form-urlencoded
// @Param flow formData string true "Pending authorization flow ID"
// @Param action formData string true "Decision" Enums(approve,deny)
// @Param remember_me formData bool false "Keep the session signed in longer"
// @Success 302
// @Failure 400,401,404 {string} string
// @Router /oauth2/consent [post]
func swaggerConsentDecision() {}

// @Summary List SAML providers
// @Tags saml
// @Security XUserAuth
// @Success 200 {object} Response[[]ConfigMeta]
// @Failure 401,500 {object} httputil.Error
// @Router /v1/saml/providers [get]
func swaggerListSAMLProviders() {}

// @Summary Get a SAML provider
// @Tags saml
// @Security XUserAuth
// @Param id path string true "Provider ID"
// @Success 200 {object} Response[ConfigResource]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/saml/providers/{id} [get]
func swaggerGetSAMLProvider() {}

// @Summary Save a SAML provider
// @Tags saml
// @Security XUserAuth
// @Param id path string true "Provider ID"
// @Param request body ConfigRequest true "SAML provider configuration"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/saml/providers/{id} [put]
func swaggerPutSAMLProvider() {}

// @Summary Delete a SAML provider
// @Tags saml
// @Security XUserAuth
// @Param id path string true "Provider ID"
// @Success 200 {object} Response[swaggerObject]
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/saml/providers/{id} [delete]
func swaggerDeleteSAMLProvider() {}

// @Summary Get SAML service-provider metadata
// @Tags saml
// @Param provider path string true "Provider ID"
// @Success 200 {string} string "SAML metadata XML"
// @Failure 404,500 {object} httputil.Error
// @Router /saml/{provider}/metadata [get]
func swaggerSAMLMetadata() {}

// @Summary Start SAML login
// @Tags saml
// @Param provider path string true "Provider ID"
// @Success 302
// @Failure 400,404,500 {object} httputil.Error
// @Router /saml/{provider}/login [get]
func swaggerSAMLLogin() {}

// @Summary Handle a SAML assertion
// @Tags saml
// @Accept application/x-www-form-urlencoded
// @Param provider path string true "Provider ID"
// @Param SAMLResponse formData string true "Encoded SAML response"
// @Success 302
// @Failure 400,401,404,500 {object} httputil.Error
// @Router /saml/{provider}/acs [post]
func swaggerSAMLACS() {}

// @Summary Preview an email template
// @Tags settings
// @Security XUserAuth
// @Param request body swaggerObject true "Unsaved template and sample data"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/email/preview [post]
func swaggerEmailPreview() {}

// @Summary Preview custom userinfo claims
// @Tags settings
// @Security XUserAuth
// @Param request body swaggerObject true "Claims, user and custom-info set"
// @Success 200 {object} Response[swaggerObject]
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/custom-info/preview [post]
func swaggerCustomInfoPreview() {}

// @Summary List LDAP group mappings
// @Tags ldap
// @Security XUserAuth
// @Success 200 {object} swaggerObject
// @Failure 401,500 {object} httputil.Error
// @Router /v1/lmaps [get]
func swaggerListLMaps() {}

// @Summary Create an LDAP group mapping
// @Tags ldap
// @Security XUserAuth
// @Param request body data.LMap true "LDAP group mapping"
// @Success 200 {object} swaggerObject
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/lmaps [post]
func swaggerCreateLMap() {}

// @Summary Get an LDAP group mapping
// @Tags ldap
// @Security XUserAuth
// @Param name path string true "Mapping name"
// @Success 200 {object} swaggerObject
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/lmaps/{name} [get]
func swaggerGetLMap() {}

// @Summary Replace an LDAP group mapping
// @Tags ldap
// @Security XUserAuth
// @Param name path string true "Mapping name"
// @Param request body data.LMap true "LDAP group mapping"
// @Success 200 {object} swaggerObject
// @Failure 400,401,404,500 {object} httputil.Error
// @Router /v1/lmaps/{name} [put]
func swaggerPutLMap() {}

// @Summary Delete an LDAP group mapping
// @Tags ldap
// @Security XUserAuth
// @Param name path string true "Mapping name"
// @Success 200 {object} swaggerObject
// @Failure 401,404,500 {object} httputil.Error
// @Router /v1/lmaps/{name} [delete]
func swaggerDeleteLMap() {}

// @Summary Check current user's access
// @Tags self-service
// @Security XUserAuth
// @Param request body data.CheckRequestUser true "Access check"
// @Success 200 {object} data.CheckResponse
// @Failure 400,401,500 {object} httputil.Error
// @Router /v1/me/check [post]
func swaggerMeCheck() {}

// @Summary Get customized OpenID Connect discovery metadata
// @Tags oidc
// @Param custom path string true "Custom info set"
// @Success 200 {object} swaggerObject
// @Router /oauth2/openid/{custom}/.well-known/openid-configuration [get]
func swaggerCustomOpenIDConfiguration() {}
