# Product

<!-- impeccable:product-schema 1 -->

Scope: the Turna `auth` middleware and its embedded management UI
(`pkg/server/http/middleware/auth`, UI source in `_ui/`). It does not cover the
rest of Turna (loader, preprocess, runner, proxy, other middlewares).

## Platform

web

## Users

Two first-class audiences share one embedded UI, separated at runtime by the
`admin` settings namespace and `GET /v1/capabilities`:

- **Platform / ops engineers (administrators).** Operate an identity provider
  for their own applications. They create users, service accounts, roles and
  permissions; register OAuth2 clients, upstream OAuth providers and SAML
  providers; wire LDAP configs and group maps; issue and revoke API keys; and
  tune every runtime namespace (tokens, JWT signing key, passkey RP, email/SMTP,
  signup, TOTP, mTLS, device flow, cache, custom userinfo). Their situation is
  operational: configuring a live auth surface, then verifying a specific
  identity really does or does not get through.
- **End users (self-service).** Reach the same UI without admin capability and
  see only their own surfaces: profile and roles (`#account`), password change,
  TOTP enrollment plus recovery codes, passkey registration, and device-flow
  approval (`#device`, `/ui/device?user_code=...`). Their situation is a single
  short errand, often arriving from a link or a CLI prompt, not an operating
  session.

Non-admin users must never be shown admin surfaces; admin tabs are hidden from
capabilities, not merely disabled.

## Product Purpose

Give an application a complete authentication and authorization plane without
that logic living in the application. The middleware is a full IdP: IAM model,
OAuth2/OIDC authorization server, LDAP and SAML federation, passkeys, TOTP,
email/magic-link login, device flow, API keys, mTLS client auth — plus the UI
that operates all of it.

Success is that an operator can stand up, change and verify auth behavior
entirely at runtime — no restart, no YAML edit, no redeploy — and that an end
user can complete a credential errand without asking an operator for help.

## Positioning

Runtime-first identity, not config-first identity. Static YAML covers only how
to reach PostgreSQL: `prefix_path`, `database`, `encryption.key`. Every real
setting lives encrypted in PostgreSQL, is edited through the API or UI, and
takes effect immediately: writes bump a version, append an event, and
`pg_notify('auth_changed', version)`; other instances converge through version
polling against an in-memory read model.

Two consequences a neighboring proxy middleware cannot truthfully copy:

- The whole IdP ships inside one Go binary with the UI embedded via `go:embed`
  and no external identity service to run.
- The middleware registers itself as an in-process token issuer, so a `session`
  provider referencing `auth_middleware` validates and refreshes tokens without
  HTTP self-calls.

## Operating Context

- **UI is the primary control plane.** Because runtime settings live in the
  database, the UI is the expected operator path; the REST API exists for
  automation, and the shipped Swagger UI (`/swagger/*`) documents it.
- The UI is served from the middleware prefix (default `/auth/ui/*`), routed
  through `session` so the auth API/UI receives an authenticated `X-User`.
- Navigation is grouped by operating concern, not by endpoint: CONTROL, SELF
  SERVICE, IAM, LDAP, FEDERATION, SYSTEM, PLATFORM
  (`_ui/src/lib/navigation.ts`).
- Operators work in dense, correctness-critical material: JSON records, Go
  `text/template` mail bodies, PEM keys and certificates, permission
  host/path/method resources, LDAP DNs, JWKS. Several pages exist purely to
  verify rather than edit — Access Check, Flows, OAuth2 Overview, email and
  custom-info previews.
- Destructive and irreversible actions are routine here: rotating the JWT
  signing key invalidates every outstanding token, an API key (`tak_...`) is
  shown exactly once and never again, revoking a key or a passkey takes effect
  on the next request.
- Break-glass is a real, supported mode: with the session chain removed and
  `admin.allow_missing_x_user` true, the UI is reached with no identity at all.
- Deployment is multi-instance; single-instance assumptions (in-memory code
  store, local state) are a documented configuration hazard, not a default.

## Capabilities and Constraints

Confirmed capabilities are enumerated in
`_documents/docs/reference/server/http/middlewares/auth.md` and mirrored by
`_ui/src/lib/api.ts` (`kindSpecs`, `settingTemplates`) — treat those two files as
the source of truth for what exists, not this record.

Constraints that bind future design work:

- **Embedded, offline-capable UI.** The build output (`_ui/dist`) is compiled
  into the Go binary through `//go:embed _ui/dist/*` (`file.go:17`). No CDN, no
  runtime font/script fetch, no external network dependency at page load. Fonts
  ship as local `@fontsource` woff2.
- **Stack: Svelte 5 + Tailwind 4** (owner decision, 2026-08). The codebase is on
  Svelte 4 + Tailwind 3 today and migrates as part of the redesign: runes
  (`$state`/`$derived`/`$props`) replace `let`/`$:`/`export let`, and Tailwind's
  CSS-first `@theme` replaces `tailwind.config.cjs`. Vite + TypeScript stay. No
  component library, no runtime dependencies beyond local font packages. Any UI
  change must survive `pnpm build` and land in `dist`.
- **Serving path.** Hash routing under `/ui` (`#account`, `#api-keys`, ...) plus
  one non-hash page, `/ui/device`, that is opened from a device with a
  `user_code` query parameter.
- **Prefix is configurable.** `prefix_path` defaults to `/auth` but is not
  guaranteed; paths must be derived from `/v1/info`, never hard-coded.
- **Capability-gated rendering.** `GET /v1/capabilities` (`is_admin`,
  `anonymous_admin`, admin permission) decides what a session may see.
- **Theme.** Light, dark and system are all supported and resolved before first
  paint by an inline script in `index.html`; both themes are shipped surfaces,
  not one theme with a fallback.
- **Terminology is API-bound.** alias, local vs non-local user, service account,
  `sync_role_ids` (managed by LDAP/claim sync, reset each run) vs `role_ids` vs
  `tmp_role_ids`, lmap, principal, namespace, `X-User`, `tak_` key prefix. Copy
  must match the API vocabulary operators see in JSON and in the docs.
- Every list endpoint pages with `_limit`/`_offset` via `rakunlabs/query`, so any
  listing surface must assume unbounded record counts.

- **English-only.** The interface ships in English; no localization is planned.
- **Usage rhythm: heavy at setup, sparse afterwards.** An operator spends hours
  in it while standing the system up, then returns occasionally. Both ends must
  work: first-run guidance that does not become clutter, and fast re-entry for
  someone who has not been here in a month.
- **Navigation structure is settled.** The seven groups and their pages stay;
  only their presentation is open (grouping affordances, search, active state).

Undecided / not established: no confirmed target for record volume at the high
end, no confirmed mobile or tablet usage scenario for the admin surfaces.

## Brand Commitments

- Product name **Turna**; this component is the `auth` middleware. The UI window
  title is `TURNA // AUTH`.
- Logo assets exist at `_documents/docs/public/assets/turna.svg` and
  `turna_light.svg` (light/dark variants).
- Public documentation lives at <https://rakunlabs.github.io/turna/>; the UI's
  DOCS tab is expected to stay consistent with it.
- No voice, tone or visual identity has been declared binding by the owner.

## Evidence on Hand

- Complete written specification of behavior:
  `_documents/docs/reference/server/http/middlewares/auth.md` (452 lines).
- Shipped OpenAPI document served at `/swagger/swagger.json`
  (`auth/files/`, `file.go`).
- Real configuration examples: `testdata/config/auth.yml`, plus
  `_documents/docs/examples/`.
- Integration tests covering routing and the embedded UI:
  `middleware-integration_test.go`, `oauth-mcp_test.go`.
- Embedded SQL migrations describing the real data model:
  `auth/migrations/*.sql`.
- Deliberately absent: no customers, testimonials, case studies, benchmarks,
  pricing, uptime figures or adoption numbers exist. Do not invent any.

## Product Principles

1. **Runtime truth over static truth.** If a setting can change without a
   restart, the interface must show its current live value and the version it
   belongs to — never a form that merely echoes what was typed.
2. **Two audiences, one build, hard separation.** Capability decides what
   exists on screen. A self-service visitor should never learn that an admin
   plane is there.
3. **Verification is a first-class job, not a debugging afterthought.** Access
   Check, Flows, OAuth2 Overview and the template previews carry equal weight
   with the editors; an operator's real question is "does this identity get
   through?"
4. **Irreversibility must be legible before the click.** One-time secrets, key
   rotation and revocation state their consequence and their scope at the point
   of action, not in a toast afterwards.
5. **Speak the API's language.** The UI is one view onto records operators also
   touch as JSON, curl and docs; invented friendly synonyms cost more than the
   jargon saves.
6. **Ship inside the binary.** Every asset is local, every page works with no
   internet reachable from the server, and the whole UI stays small enough to
   embed.
