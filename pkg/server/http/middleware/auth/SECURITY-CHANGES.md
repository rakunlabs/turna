# Authentication hardening notes

## Client compatibility

- Password and `email_code` grants require a second factor when the user has
  confirmed TOTP enabled. Send `totp` with either a current authenticator code
  or an unused recovery code. `401 mfa_required` means the UI should ask for it
  and retry. Missing/invalid MFA does **not** consume the email login code.
- Authenticator time steps are single-use, including the step used to confirm
  enrollment. Wait for the next code after enrollment rather than reusing it.
- Five invalid second-factor attempts lock verification for five minutes,
  shared across grants and replicas. During cooldown the API returns 429 and
  `Retry-After: 300`. Missing codes do not spend the attempt budget. This is
  **not** a replacement for rate limiting password, email-send and other public
  authentication endpoints at the gateway.
- Dynamically registered clients are limited to their registered `grant_types`.
  Existing dynamic clients without stored grant metadata get the old default
  grants (`authorization_code`, `refresh_token`), not password or token exchange.
  Configured registration lifetime is checked on lookup, not just on cleanup.
- Userinfo and introspection reject tokens whose user is disabled or deleted.
- Explicit grants on an owned API key are limited to the owner's **current**
  expanded access. Revoked/expired temporary access is not retained by the key.
  Ownerless administrator-created keys remain independent. Owned keys cannot
  override their owner's identity email through arbitrary key metadata.

## Alias migration

Migration `14_user_aliases.sql` creates an authoritative unique alias registry
maintained by database triggers, including writes outside the application.
Email aliases are trimmed and case-insensitive. Usernames keep their existing
case semantics. Blank entries are removed and duplicates on the same account
are collapsed. The `api-key:` namespace is reserved for virtual principals.

The migration refuses cross-account collisions instead of merging or deleting
accounts. Resolve the ownership of conflicting aliases before retrying it.
Back up the database and test migrations against a copy before deployment.

## Write-only credentials

Management GETs for settings, OAuth clients/providers, LDAP/SAML configuration
and service accounts no longer return stored passwords, private keys or client
secrets. They return an empty/absent value and a `*_set` boolean instead.

On configuration/settings writes, omitted or blank secret fields retain the
stored credential. Enter a new nonempty value to replace it; an explicit JSON
`null` clears a configuration secret. The JWT private key cannot be cleared by
management PUT: use key replacement or rotation. For user/service-account
credential fields, omitted or blank values preserve the existing value.

The admin-protected `/v1/session-providers` **runtime export** deliberately
still carries client credentials: remote session middleware needs them to
perform token operations. It is not a UI settings GET; limit access to trusted
runtime callers. Public JWKS continues to contain only public key material.

## Management console drafts

- Settings have separate draft and last-read server values. Navigation and
  refresh preserve unsaved drafts; the banner lists them. Use the guarded
  **Discard drafts** action to restore the last-read values without writing.
- A successful write followed by a failed read is reported as saved with a
  refresh warning, not a failed write. Entered credentials are cleared after a
  successful settings save even if that read fails.
- Existing user/service-account edits use PATCH with only changed fields.
  `details_patch` updates named profile fields (null removes a field); the
  original `details` replacement and PUT APIs keep their existing semantics.
  This prevents untouched LDAP roles, temporary access and profile fields from
  being overwritten by an older UI copy.
- Copying a masked configuration record to a new identifier, or moving/renaming
  a session provider, requires re-entering any stored credentials at the new
  location. A write-only secret cannot be copied out of the browser's empty field.

Run console state regression tests with `pnpm test` inside `_ui`. They compile
the actual client-side Svelte runes using the existing build toolchain.

## Concurrent administration

Migration `15_record_revisions.sql` assigns a new opaque per-record revision
on every DB write, including directory sync and direct SQL. It covers settings,
OAuth clients/providers, LDAP/SAML configs, users/service accounts, roles,
permissions and LDAP group maps. Record GETs include `revision` and an `ETag`.

The console sends `If-Match: "rev-<number>"` for loaded records, or
`If-None-Match: *` when creating an absent setting/config. The DB checks this
condition under a lock in the write transaction. A stale update/delete or an
unexpected concurrent creation returns **412 Precondition Failed** without
changing the record. An unrelated record's update does not cause a conflict.
Deleting and recreating a record does not reuse its revision.

On conflict the console keeps the draft. Copy the desired edits, re-read the
current record, and reapply them; it does not automatically retry or force-save.
Settings background refreshes never replace the revision associated with a
dirty draft. After a successful settings write whose follow-up read failed,
another save requires a successful re-read first.

Conditions are optional for existing non-UI API integrations: requests without
these headers keep their original unconditional behavior. External management
UIs should opt in by echoing the returned revision; cross-origin CORS policies
must allow `If-Match` / `If-None-Match` headers. They can use the JSON revision
without exposing `ETag` through CORS. Separate passkey and linked-account
management endpoints are not covered by this record revision scheme.

## API-key and device UI follow-up

- API-key metadata already has a numeric revision. Admin and owner-scoped
  update/delete requests may send `If-Match: "keyrev-<revision>"`; the console
  does so automatically. Stale writes return 412, preserving the edit draft.
  The read/merge/write path also uses a DB compare-and-swap to avoid races
  between two simultaneous metadata updates. No additional migration is needed.
- Key issuance, amendment and revocation report the write outcome separately
  from a subsequent list refresh. Failed reads preserve the last successful
  list and show an error/retry state rather than claiming the registry is empty.
  Issue/validation status in the list uses committed policy, not an unsaved
  settings switch. Owner-access read errors are visible and retryable.
- Unacknowledged API keys, recovery codes and pending TOTP enrollment material
  remain in this tab's memory across console page navigation. A banner links
  back to them, and closing/reloading the tab warns about losing them. They
  are never written to localStorage/sessionStorage and are cleared on an
  observed identity change. A second issuance cannot replace an unacknowledged
  key or recovery set. Use the explicit stored/acknowledge action to clear it.
- Device code entry is locked during lookup/approval. Responses are also
  checked against the code still on screen before enabling a decision.
