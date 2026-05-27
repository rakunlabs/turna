# auth

`auth` was a legacy OAuth middleware and is not registered by the current HTTP middleware registry.

Use the current authentication middlewares instead:

- [`session`](./session) validates bearer tokens or session-stored access tokens.
- [`login`](./login) provides browser login, OAuth2 code flow, password flow, and logout helpers.
- [`oauth2`](./oauth2) exposes OAuth2/OIDC-compatible endpoints backed by `iam`.
- [`iam`](./iam), [`iam_check`](./iam_check), and [`iam_forward_auth`](./iam_forward_auth) provide local IAM and authorization checks.

Existing configurations that still use `auth:` should be migrated to `session` plus `login`, or to the IAM/OAuth2 stack depending on the use case.
