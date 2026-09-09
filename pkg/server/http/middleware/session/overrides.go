package session

// ProviderEndpointOverride replaces non-empty endpoint fields of an existing
// provider. It is instance-local configuration, keyed by provider name.
type ProviderEndpointOverride struct {
	Oauth2 OAuth2EndpointOverride `cfg:"oauth2"`
}

type OAuth2EndpointOverride struct {
	AuthURL          string `cfg:"auth_url"`
	TokenURL         string `cfg:"token_url"`
	UserInfoURL      string `cfg:"userinfo_url"`
	CertURL          string `cfg:"cert_url"`
	IntrospectURL    string `cfg:"introspect_url"`
	RevocationURL    string `cfg:"revocation_url"`
	LogoutURL        string `cfg:"logout_url"`
	PasskeyURL       string `cfg:"passkey_url"`
	APIKeyURL        string `cfg:"api_key_url"`
	SignupURL        string `cfg:"signup_url"`
	PasswordResetURL string `cfg:"password_reset_url"`
}

// ApplyProviderEndpointOverrides returns a read-only provider map without
// modifying the source map or its OAuth2 settings. Unknown names are ignored;
// overrides never create providers or enable OAuth2 on a non-OAuth2 provider.
func ApplyProviderEndpointOverrides(providers map[string]Provider, overrides map[string]ProviderEndpointOverride) map[string]Provider {
	if len(overrides) == 0 || providers == nil {
		return providers
	}

	result := make(map[string]Provider, len(providers))
	for name, provider := range providers {
		if override, ok := overrides[name]; ok && provider.Oauth2 != nil {
			oauth := *provider.Oauth2
			for _, field := range []struct {
				dst *string
				src string
			}{
				{&oauth.AuthURL, override.Oauth2.AuthURL},
				{&oauth.TokenURL, override.Oauth2.TokenURL},
				{&oauth.UserInfoURL, override.Oauth2.UserInfoURL},
				{&oauth.CertURL, override.Oauth2.CertURL},
				{&oauth.IntrospectURL, override.Oauth2.IntrospectURL},
				{&oauth.RevocationURL, override.Oauth2.RevocationURL},
				{&oauth.LogoutURL, override.Oauth2.LogoutURL},
				{&oauth.PasskeyURL, override.Oauth2.PasskeyURL},
				{&oauth.APIKeyURL, override.Oauth2.APIKeyURL},
				{&oauth.SignupURL, override.Oauth2.SignupURL},
				{&oauth.PasswordResetURL, override.Oauth2.PasswordResetURL},
			} {
				if field.src != "" {
					*field.dst = field.src
				}
			}
			provider.Oauth2 = &oauth
		}
		result[name] = provider
	}
	return result
}

// ApplyProviderGroupEndpointOverrides applies the same provider-name overrides
// to every group, including inherited providers, without mutating the catalog.
func ApplyProviderGroupEndpointOverrides(groups map[string]map[string]Provider, overrides map[string]ProviderEndpointOverride) map[string]map[string]Provider {
	if len(overrides) == 0 || groups == nil {
		return groups
	}
	result := make(map[string]map[string]Provider, len(groups))
	for name, providers := range groups {
		result[name] = ApplyProviderEndpointOverrides(providers, overrides)
	}
	return result
}
