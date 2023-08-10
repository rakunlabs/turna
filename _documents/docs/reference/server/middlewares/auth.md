# auth

Authentication middleware to redirect and check the oauth2 token.

```yaml
middlewares:
  test:
    auth:
      provider:
        active: "" # default provider, if empty then use the first provider, set to 'noop' to disable auth check
        keycloak: # keycloak provider
          client_id: ""
          client_id_external: ""
          client_secret: ""
          client_secret_external: ""
          scopes: []
          cert_url: "" # if introspect_url exist then cert_url not usable for validate the token, if empty then generating with using realm and base_url
          introspect_url: "" # use when cert_url not possible for validate the token
          auth_url: "" # authentication url, if empty then generating with using realm and base_url
          auth_url_external: "" # reaching page from outside, default is auth_url
          token_url: "" # token url, if empty then generating with using realm and base_url
          token_url_external: "" # reaching page from outside, default is token_url
          base_url: "" # base url, example: https://keycloak:8080/auth/
          realm: "" # realm name
        generic: # generic oauth2 provider
          client_id: ""
          client_id_external: ""
          client_secret: ""
          client_secret_external: ""
          scopes: []
          cert_url: "" # if introspect_url exist then cert_url not usable for validate the token
          introspect_url: "" # use when cert_url not possible for validate the token
          auth_url: "" # authentication url
          auth_url_external: "" # reaching page from outside, default is auth_url
          token_url: "" # token url
          token_url_external: "" # reaching page from outside, default is token_url
      redirect:
        cookie_name: "" # cookie name for store token, default is "auth_" + ClientID
        max_age: 0 # number of seconds until the cookie expires
        path: "" # cookie path, path that must exist in the requested URL for the browser to send the Cookie header
        domain: "" # cookie domain, domain for which the cookie will be sent
        secure: false # secure flag for the cookie
        same_site: 0 # same site flag for the cookie for Lax 2, Strict 3, None 4
        http_only: false # http only flag for the cookie, for true for not accessible by JavaScript
        no_client_id_param: false # use to not add client_id in the query params
        callback: "" # callback url
        base_url: "" # base url, to use for the redirect, default is the request Host with checking the X-Forwarded-Host header
        schema: "" # default schema to use for the redirect if no schema is provided, default is the https
        use_session: false # use session for store token instead of cookie
        session_key: "" # session key for store token, if empty generating random key
        token_header: false # set to the header of the token as Bearer
        refresh_token: false # use to refresh the token if it is expired or 10s before expire
        check_value: "" # value to check in the context (combined with other middlewares) EXPERIMENTAL
        check_agent: false # check if the request is a browser redirect to the auth_url
      skip_suffixes: [] # skip suffixes for auth check, example "/ping", "/health", "/metrics"
```
