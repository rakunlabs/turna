<script lang="ts">
  import Section from "../ui/Section.svelte";
  import TemporaryAccessPanel from "./TemporaryAccessPanel.svelte";
  import PasskeyPanel from "./PasskeyPanel.svelte";
  import { editor } from "../../lib/state/editor.svelte";

  /**
   * The field view of the draft. Every control here writes straight into the
   * one document the raw exhibit shows — there is no second copy of the record
   * and no field that only exists in this form.
   */
  type Line = {
    label: string;
    value: string;
    set: (value: string) => void;
    placeholder?: string;
    hint?: string;
    type?: "text" | "number";
    min?: string;
    mono?: boolean;
    wide?: boolean;
  };

  type Exhibit = {
    label: string;
    value: string;
    set: (value: string) => void;
    placeholder?: string;
    hint?: string;
    rows?: number;
    /** JSON documents are parsed on blur, so half-typed text is never rejected. */
    lazy?: boolean;
  };

  type Toggle = {
    label: string;
    on: boolean;
    set: (value: boolean) => void;
    hint?: string;
    /** True when the ON state widens what this instance accepts. */
    consequential?: boolean;
  };

  function slug(label: string) {
    return `f-${label
      .toLowerCase()
      .replace(/[^a-z0-9]+/g, "-")
      .replace(/^-|-$/g, "")}`;
  }

  const namespace = $derived(editor.kind === "settings" ? editor.loadedID || editor.id : "");
  const codeStore = $derived(editor.getPathString(["code_store", "active"]).trim() || "memory");
  const redisTLS = $derived(editor.getPathBool(["code_store", "redis", "tls", "enabled"]));
  const localUser = $derived(editor.getBool("local", true));
  const resources = $derived(editor.resources());

  let mtlsCertPEM = $state("");
  let mtlsCertError = $state("");

  function certificateBytes(value: string) {
    const cleaned = value
      .replace(/-----BEGIN CERTIFICATE-----/g, "")
      .replace(/-----END CERTIFICATE-----/g, "")
      .replace(/\s+/g, "");
    if (!cleaned) throw new Error("Certificate is empty");

    const binary = atob(cleaned);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }

    return bytes;
  }

  async function useMTLSCertificateFingerprint() {
    mtlsCertError = "";
    try {
      const digest = await crypto.subtle.digest("SHA-256", certificateBytes(mtlsCertPEM));
      const hex = Array.from(new Uint8Array(digest))
        .map((item) => item.toString(16).padStart(2, "0"))
        .join("");
      editor.setNestedString("details", "cert_fingerprint", hex);
      mtlsCertPEM = "";
    } catch (err) {
      mtlsCertError = err instanceof Error ? err.message : "Cannot read certificate";
    }
  }
</script>

{#snippet line(f: Line)}
  <div class={f.wide ? "min-w-0 sm:col-span-2" : "min-w-0"}>
    <label class="stamp block" for={slug(f.label)}>{f.label}</label>
    <input
      id={slug(f.label)}
      class="entry mt-1.5 {f.mono ? 'serial' : ''}"
      type={f.type ?? "text"}
      min={f.min}
      placeholder={f.placeholder ?? ""}
      autocomplete="off"
      spellcheck="false"
      value={f.value}
      oninput={(event) => f.set(event.currentTarget.value)}
    />
    {#if f.hint}
      <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">{f.hint}</p>
    {/if}
  </div>
{/snippet}

{#snippet exhibit(f: Exhibit)}
  <div class="min-w-0 sm:col-span-2">
    <label class="stamp block" for={slug(f.label)}>{f.label}</label>
    <textarea
      id={slug(f.label)}
      class="exhibit mt-1.5"
      rows={f.rows ?? 6}
      placeholder={f.placeholder ?? ""}
      spellcheck="false"
      value={f.value}
      oninput={(event) => {
        if (!f.lazy) f.set(event.currentTarget.value);
      }}
      onchange={(event) => {
        if (f.lazy) f.set(event.currentTarget.value);
      }}
    ></textarea>
    {#if f.hint}
      <p class="mt-1.5 max-w-[70ch] text-[12px] leading-[1.5] text-muted">{f.hint}</p>
    {/if}
  </div>
{/snippet}

{#snippet toggle(f: Toggle)}
  <div class="min-w-0">
    <button
      type="button"
      role="switch"
      aria-checked={f.on}
      class="flex w-full cursor-pointer items-center gap-3 py-1 text-left"
      onclick={() => f.set(!f.on)}
    >
      <span
        class="relative inline-flex h-[18px] w-[34px] shrink-0 items-center border transition-colors duration-150
          {f.on
          ? f.consequential
            ? 'border-seal bg-seal'
            : 'border-carbon bg-carbon'
          : 'border-rule bg-transparent'}"
      >
        <span
          class="absolute top-[2px] h-[12px] w-[12px] transition-[left] duration-150 ease-[var(--ease-settle)]
            {f.on ? 'left-[19px] bg-white' : 'left-[2px] bg-faint'}"
        ></span>
      </span>
      <span class="min-w-0 text-[13.5px] leading-[1.45] text-ink">{f.label}</span>
    </button>
    {#if f.hint}
      <p class="ml-[46px] max-w-[62ch] text-[12px] leading-[1.5] text-muted">{f.hint}</p>
    {/if}
  </div>
{/snippet}

{#snippet note(text: string)}
  <p class="max-w-[70ch] text-[13px] leading-[1.6] text-muted sm:col-span-2">{text}</p>
{/snippet}

{#if editor.kind === "settings"}
  {#if namespace === "admin"}
    <Section title="Administration gate" note="Who is allowed to change this instance.">
      <div class="grid gap-6 sm:grid-cols-2">
        {@render line({
          label: "Admin permission",
          value: editor.getString("permission"),
          set: (value) => editor.setString("permission", value),
          placeholder: "turna.auth.admin",
          hint: "Matched against the permission ID or name carried on X-User. Leaving it empty keeps bootstrap compatibility — every authenticated request can administer auth.",
          wide: true,
        })}
        {@render toggle({
          label: "Allow missing X-User break-glass admin",
          on: editor.getBool("allow_missing_x_user", true),
          set: (value) => editor.setBool("allow_missing_x_user", value),
          consequential: true,
          hint: "Requests without a session chain are treated as administrators.",
        })}
        {@render note(
          "Use break-glass only while the auth route is not publicly exposed. With it on, removing the session chain in front of this instance lets direct requests administer auth.",
        )}
      </div>
    </Section>
  {:else if namespace === "cache"}
    <Section title="Convergence" note="How this instance notices writes made on another one.">
      <div class="grid gap-6 sm:grid-cols-2">
        {@render line({
          label: "Poll interval",
          value: editor.getPathString(["poll_interval"]),
          set: (value) => editor.setPathValue(["poll_interval"], value),
          placeholder: "5s",
          hint: "How often this instance re-reads the auth version.",
        })}

        <div class="min-w-0">
          <label class="stamp block" for="code-store">OAuth code store</label>
          <select
            id="code-store"
            class="entry mt-1.5"
            value={codeStore}
            onchange={(event) => editor.setPathValue(["code_store", "active"], event.currentTarget.value)}
          >
            <option value="memory">memory</option>
            <option value="redis">redis</option>
          </select>
          <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
            Redis is what multi-instance authorization-code, provider-state, device and email flows
            need; memory only works when a single instance serves every step.
          </p>
        </div>
      </div>
    </Section>

    {#if codeStore === "redis"}
      <Section title="Redis" note="Connection used for the shared code store.">
        <div class="grid gap-6 sm:grid-cols-2">
          {@render line({
            label: "Redis addresses",
            value: editor.getPathList(["code_store", "redis", "address"]),
            set: (value) => editor.setPathList(["code_store", "redis", "address"], value),
            placeholder: "127.0.0.1:6379",
            hint: "Comma separated for a cluster.",
            mono: true,
            wide: true,
          })}
          {@render line({
            label: "Redis username",
            value: editor.getPathString(["code_store", "redis", "username"]),
            set: (value) => editor.setPathValue(["code_store", "redis", "username"], value),
            placeholder: "optional",
          })}
          {@render line({
            label: "Redis password",
            value: editor.getPathString(["code_store", "redis", "password"]),
            set: (value) => editor.setPathValue(["code_store", "redis", "password"], value),
            placeholder: "optional",
          })}
          {@render line({
            label: "Client name",
            value: editor.getPathString(["code_store", "redis", "client_name"]),
            set: (value) => editor.setPathValue(["code_store", "redis", "client_name"], value),
            placeholder: "turna-auth",
          })}
          {@render toggle({
            label: "Redis TLS",
            on: redisTLS,
            set: (value) => editor.setPathValue(["code_store", "redis", "tls", "enabled"], value),
          })}

          {#if redisTLS}
            {@render line({
              label: "TLS CA file",
              value: editor.getPathString(["code_store", "redis", "tls", "ca_file"]),
              set: (value) => editor.setPathValue(["code_store", "redis", "tls", "ca_file"], value),
              placeholder: "optional",
              mono: true,
            })}
            {@render line({
              label: "TLS cert file",
              value: editor.getPathString(["code_store", "redis", "tls", "cert_file"]),
              set: (value) => editor.setPathValue(["code_store", "redis", "tls", "cert_file"], value),
              placeholder: "optional",
              mono: true,
            })}
            {@render line({
              label: "TLS key file",
              value: editor.getPathString(["code_store", "redis", "tls", "key_file"]),
              set: (value) => editor.setPathValue(["code_store", "redis", "tls", "key_file"], value),
              placeholder: "optional",
              mono: true,
            })}
          {/if}
        </div>
      </Section>
    {/if}
  {:else if namespace === "device"}
    <Section title="Device flow" note="The browserless grant used by CLIs and set-top devices.">
      <div class="grid gap-6 sm:grid-cols-2">
        {@render toggle({
          label: "Disable device flow",
          on: editor.getBool("disabled"),
          set: (value) => editor.setBool("disabled", value),
          hint: "Device authorization requests are refused while this is on.",
        })}
        {@render line({
          label: "Code lifetime",
          value: editor.getString("code_lifetime"),
          set: (value) => editor.setString("code_lifetime", value),
          placeholder: "10m",
          hint: "How long a user has to approve a pending device.",
        })}
        {@render line({
          label: "Poll interval (seconds)",
          value: String(editor.getPathNumber(["interval"], 5)),
          set: (value) => editor.setPathNumber(["interval"], value),
          type: "number",
          min: "1",
          hint: "How often the device may ask whether it has been approved.",
        })}
        {@render line({
          label: "Verification URI",
          value: editor.getString("verification_uri"),
          set: (value) => editor.setString("verification_uri", value),
          placeholder: "default: <prefix>/ui/device",
          hint: "Shown to the person approving the device. Empty uses this instance's own page.",
          wide: true,
        })}
      </div>
    </Section>
  {:else if namespace === "token_exchange"}
    <Section title="Token exchange" note="RFC 8693 exchange of one token for another.">
      <div class="grid gap-6 sm:grid-cols-2">
        {@render toggle({
          label: "Disable token exchange",
          on: editor.getBool("disabled"),
          set: (value) => editor.setBool("disabled", value),
          hint: "Exchange requests are refused while this is on.",
        })}
      </div>
    </Section>
  {:else if namespace === "totp"}
    <Section title="TOTP" note="Time-based one-time codes as a second factor.">
      <div class="grid gap-6 sm:grid-cols-2">
        {@render toggle({
          label: "Disable TOTP",
          on: editor.getBool("disabled"),
          set: (value) => editor.setBool("disabled", value),
          hint: "Enrolment and verification both stop while this is on.",
        })}
        {@render line({
          label: "Issuer",
          value: editor.getString("issuer"),
          set: (value) => editor.setString("issuer", value),
          placeholder: "Turna Auth",
          hint: "The name authenticator apps show beside the code.",
        })}
        {@render line({
          label: "Skew periods",
          value: String(editor.getPathNumber(["skew"], 1)),
          set: (value) => editor.setPathNumber(["skew"], value),
          type: "number",
          min: "0",
          hint: "How many 30-second steps either side of now are still accepted.",
        })}
      </div>
    </Section>
  {/if}
{:else if editor.kind === "clients"}
  <Section
    title="Credentials"
    note="What this client presents at the token endpoint. A client ID that is an HTTPS URL (OAuth Client ID Metadata Document, e.g. Claude Code) is fetched live; a record saved under that URL only overlays allowed resources, scopes, skip consent and roles claim — redirect URIs stay in the live document."
  >
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Client secret",
        value: editor.getString("client_secret"),
        set: (value) => editor.setString("client_secret", value),
        placeholder: "change-me",
      })}
      {@render line({
        label: "Scopes",
        value: editor.getList("scope"),
        set: (value) => editor.setList("scope", value),
        placeholder: "openid, profile",
        hint: "Comma or newline separated.",
      })}
      {@render exhibit({
        label: "Redirect / whitelist URLs",
        value: editor.getList("whitelist_urls"),
        set: (value) => editor.setList("whitelist_urls", value),
        placeholder: "https://app.example.com/callback",
        rows: 4,
        hint: "One per line. A redirect that is not listed here is refused.",
      })}
      {@render exhibit({
        label: "Allowed resources",
        value: editor.getList("resources"),
        set: (value) => editor.setList("resources", value),
        placeholder: "https://app.example.com/krabby/mcp",
        rows: 4,
        hint: "RFC 8707 resource indicators this client may request (one per line, prefix match). Empty allows any resource. Granted resources become token audiences (aud).",
      })}
      {@render toggle({
        label: "Skip consent",
        on: editor.getBool("skip_consent"),
        set: (value) => editor.setBool("skip_consent", value),
        hint: "Auto-approve authorization requests after login. Enable only for trusted first-party clients.",
        consequential: true,
      })}
    </div>
  </Section>
{:else if editor.kind === "providers"}
  <Section title="Upstream client" note="How this instance identifies itself to the provider.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Upstream client ID",
        value: editor.getString("client_id"),
        set: (value) => editor.setString("client_id", value),
        placeholder: "turna",
      })}
      {@render line({
        label: "Upstream client secret",
        value: editor.getString("client_secret"),
        set: (value) => editor.setString("client_secret", value),
        placeholder: "change-me",
      })}
      {@render line({
        label: "Scopes",
        value: editor.getList("scopes"),
        set: (value) => editor.setList("scopes", value),
        placeholder: "openid, profile, email",
      })}
    </div>
  </Section>

  <Section title="Endpoints" note="Taken from the provider's own discovery document.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Auth URL",
        value: editor.getString("auth_url"),
        set: (value) => editor.setString("auth_url", value),
        placeholder: "https://idp.example.com/auth",
        mono: true,
      })}
      {@render line({
        label: "Token URL",
        value: editor.getString("token_url"),
        set: (value) => editor.setString("token_url", value),
        placeholder: "https://idp.example.com/token",
        mono: true,
      })}
      {@render line({
        label: "Cert / JWKS URL",
        value: editor.getString("cert_url"),
        set: (value) => editor.setString("cert_url", value),
        placeholder: "https://idp.example.com/certs",
        mono: true,
      })}
      {@render line({
        label: "Userinfo URL",
        value: editor.getString("userinfo_url"),
        set: (value) => editor.setString("userinfo_url", value),
        placeholder: "optional",
        mono: true,
      })}
      {@render line({
        label: "Introspect URL",
        value: editor.getString("introspect_url"),
        set: (value) => editor.setString("introspect_url", value),
        placeholder: "optional",
        mono: true,
      })}
      {@render line({
        label: "Revocation URL",
        value: editor.getString("revocation_url"),
        set: (value) => editor.setString("revocation_url", value),
        placeholder: "optional",
        mono: true,
      })}
      {@render line({
        label: "Logout URL",
        value: editor.getString("logout_url"),
        set: (value) => editor.setString("logout_url", value),
        placeholder: "optional",
        mono: true,
        wide: true,
      })}
    </div>
  </Section>

  <Section
    title="Auto-register and role mapping"
    note="What happens the first time somebody arrives from this provider."
  >
    <div class="grid gap-6 sm:grid-cols-2">
      {@render toggle({
        label: "Register unknown users on first login",
        on: editor.getPathBool(["claim_mapping", "register"]),
        set: (value) => editor.setPathValue(["claim_mapping", "register"], value),
        consequential: true,
        hint: "A first-time login creates a non-local user from the provider claims.",
      })}
      {@render toggle({
        label: "Resolve roles via LDAP group maps",
        on: editor.getPathBool(["claim_mapping", "use_lmap"]),
        set: (value) => editor.setPathValue(["claim_mapping", "use_lmap"], value),
      })}
      {@render line({
        label: "Roles claim",
        value: editor.getPathString(["claim_mapping", "roles_claim"]),
        set: (value) => editor.setPathValue(["claim_mapping", "roles_claim"], value),
        placeholder: "groups / realm_access.roles",
        mono: true,
        wide: true,
      })}
      {@render note(
        "Map claim values to roles through LDAP group maps, or through role_map in the raw document.",
      )}
    </div>
  </Section>
{:else if editor.kind === "saml"}
  <Section title="Identity provider" note="Where the assertions come from.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "IdP metadata URL",
        value: editor.getString("metadata_url"),
        set: (value) => editor.setString("metadata_url", value),
        placeholder: "https://idp.example.com/metadata",
        mono: true,
        wide: true,
      })}
      {@render line({
        label: "SP entity ID",
        value: editor.getString("entity_id"),
        set: (value) => editor.setString("entity_id", value),
        placeholder: "optional",
      })}
      {@render line({
        label: "Alias attribute",
        value: editor.getString("alias_attribute"),
        set: (value) => editor.setString("alias_attribute", value),
        placeholder: "email / NameID fallback",
        hint: "Which assertion attribute becomes the login alias.",
      })}
      {@render toggle({
        label: "Sign authnrequests",
        on: editor.getBool("sign_requests"),
        set: (value) => editor.setBool("sign_requests", value),
      })}
      {@render exhibit({
        label: "Inline IdP metadata XML",
        value: editor.getString("metadata_xml"),
        set: (value) => editor.setString("metadata_xml", value),
        placeholder: "optional; takes precedence over the metadata URL",
        rows: 8,
      })}
      {@render exhibit({
        label: "Claim mapping JSON",
        value: editor.getJSON("claim_mapping"),
        set: (value) => editor.setJSON("claim_mapping", value),
        lazy: true,
        rows: 8,
        placeholder: '{\n  "roles_claim": "groups",\n  "use_lmap": true,\n  "role_map": {},\n  "register": true\n}',
        hint: "Maps SAML assertion attributes onto synced roles. role_map values are role names or IDs.",
      })}
      {@render note(
        `Register this provider's SP metadata at /auth/saml/${editor.id || "provider"}/metadata. Less common SAML options live in the raw document.`,
      )}
    </div>
  </Section>
{:else if editor.kind === "ldap"}
  <Section title="Directory" note="The connection used for password checks and group sync.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "LDAP address",
        value: editor.getString("addr"),
        set: (value) => editor.setString("addr", value),
        placeholder: "ldap://ldap.example.com:389",
        mono: true,
      })}
      {@render line({
        label: "Sync duration",
        value: editor.getString("sync_duration"),
        set: (value) => editor.setString("sync_duration", value),
        placeholder: "10m",
        hint: "How often the directory is re-read.",
      })}
      {@render line({
        label: "Bind username",
        value: editor.getNestedString("bind", "username"),
        set: (value) => editor.setNestedString("bind", "username", value),
        placeholder: "cn=readonly,dc=example,dc=com",
        mono: true,
      })}
      {@render line({
        label: "Bind password",
        value: editor.getNestedString("bind", "password"),
        set: (value) => editor.setNestedString("bind", "password", value),
        placeholder: "change-me",
      })}
      {@render line({
        label: "User base DN",
        value: editor.getString("user_base_dn"),
        set: (value) => editor.setString("user_base_dn", value),
        placeholder: "ou=people,dc=example,dc=com",
        mono: true,
        wide: true,
      })}
    </div>
  </Section>

  <Section title="Groups" note="The first group filter only — add further filters in the raw document.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Group base DN",
        value: editor.getFirstString("groups", "base_dn"),
        set: (value) => editor.setFirstString("groups", "base_dn", value),
        placeholder: "ou=groups,dc=example,dc=com",
        mono: true,
      })}
      {@render line({
        label: "Group filter",
        value: editor.getFirstString("groups", "filter"),
        set: (value) => editor.setFirstString("groups", "filter", value),
        placeholder: "(objectClass=groupOfUniqueNames)",
        mono: true,
      })}
      {@render line({
        label: "Group attributes",
        value: editor.getFirstList("groups", "attributes"),
        set: (value) => editor.setFirstList("groups", "attributes", value),
        placeholder: "cn, uniqueMember, description",
        mono: true,
        wide: true,
      })}
      {@render toggle({
        label: "Disable sync",
        on: editor.getBool("disable_sync"),
        set: (value) => editor.setBool("disable_sync", value),
        hint: "Group membership stops being pulled; existing sync roles stay as they are.",
      })}
    </div>
  </Section>
{:else if editor.kind === "users"}
  <Section title="Identity" note="What this person logs in with and how they are named.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: `Aliases / login IDs${editor.loadedID ? "" : " *"}`,
        value: editor.getList("alias"),
        set: (value) => editor.setList("alias", value),
        placeholder: "user@example.com, user",
        hint: "Every alias is a working login identifier. Comma or newline separated.",
        wide: true,
      })}
      {@render line({
        label: `Name${editor.isNew && localUser ? " *" : ""}`,
        value: editor.getNestedString("details", "name"),
        set: (value) => editor.setNestedString("details", "name", value),
        placeholder: "User Name",
      })}
      {@render line({
        label: "Email",
        value: editor.getNestedString("details", "email"),
        set: (value) => editor.setNestedString("details", "email", value),
        placeholder: "user@example.com",
      })}
      {@render line({
        label: "Uid",
        value: editor.getNestedString("details", "uid"),
        set: (value) => editor.setNestedString("details", "uid", value),
        placeholder: "user",
      })}
      {@render toggle({
        label: "Local user",
        on: localUser,
        set: (value) => editor.setLocalUser(value),
        hint: "Local users carry a stored password. Turning this off clears it — the account then only authenticates upstream.",
      })}
      {#if localUser}
        {@render line({
          label: `Password${editor.loadedID ? "" : " *"}`,
          value: editor.getNestedString("details", "password"),
          set: (value) => editor.setNestedString("details", "password", value),
          placeholder: "leave empty to keep the existing password",
          hint: "Stored bcrypt hashed; it is never read back.",
        })}
      {/if}
    </div>
  </Section>

  <Section title="Standing access" note="Grants that do not expire.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Role IDs",
        value: editor.getList("role_ids"),
        set: (value) => editor.setList("role_ids", value),
        placeholder: "admin, operator",
        mono: true,
      })}
      {@render line({
        label: "Sync role IDs",
        value: editor.getList("sync_role_ids"),
        set: (value) => editor.setList("sync_role_ids", value),
        placeholder: "ldap-admin",
        mono: true,
        hint: "Written by LDAP sync; edits here are overwritten on the next sync.",
      })}
      {@render line({
        label: "Permission IDs",
        value: editor.getList("permission_ids"),
        set: (value) => editor.setList("permission_ids", value),
        placeholder: "read-api",
        mono: true,
      })}
      {@render toggle({
        label: "Active",
        on: editor.getBool("is_active", true),
        set: (value) => editor.setBool("is_active", value),
        hint: "Turning this off stops every login and every token for this user on the next request.",
      })}
    </div>
  </Section>
{:else if editor.kind === "service-accounts"}
  <Section title="Identity" note="The machine identity used for client_credentials.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: `Aliases / client IDs${editor.loadedID ? "" : " *"}`,
        value: editor.getList("alias"),
        set: (value) => editor.setList("alias", value),
        placeholder: "my-service",
        hint: "The first alias becomes the client ID.",
        wide: true,
      })}
      {@render line({
        label: `Name${editor.loadedID ? "" : " *"}`,
        value: editor.getNestedString("details", "name"),
        set: (value) => editor.setNestedString("details", "name", value),
        placeholder: "my-service",
      })}
      {@render line({
        label: `Client secret${
          editor.loadedID ||
          editor.getNestedString("details", "secret") ||
          editor.getNestedString("details", "cert_fingerprint") ||
          editor.getNestedString("details", "cert_subject")
            ? ""
            : " *"
        }`,
        value: editor.getNestedString("details", "secret"),
        set: (value) => editor.setNestedString("details", "secret", value),
        placeholder: "optional for mTLS-only clients",
        hint: editor.loadedID
          ? "This is the stored client secret. Change it here to rotate the credential."
          : "Either a secret or an mTLS certificate binding is required.",
      })}
      {@render line({
        label: "Default scope",
        value: editor.getNestedString("details", "scope"),
        set: (value) => editor.setNestedString("details", "scope", value),
        placeholder: "openid profile",
      })}
    </div>
  </Section>

  <Section
    title="mTLS certificate"
    note="With global mTLS enabled this account can use grant_type=client_credentials without a secret, as long as the presented certificate matches one of these fields."
  >
    {#snippet aside()}
      <span class="stamp">client_id = first alias</span>
    {/snippet}

    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Cert sha256 fingerprint",
        value: editor.getNestedString("details", "cert_fingerprint"),
        set: (value) => editor.setNestedString("details", "cert_fingerprint", value),
        placeholder: "lowercase sha256 hex",
        mono: true,
        wide: true,
      })}
      {@render line({
        label: "Cert subject",
        value: editor.getNestedString("details", "cert_subject"),
        set: (value) => editor.setNestedString("details", "cert_subject", value),
        placeholder: "CN=my-client,O=Example",
        mono: true,
        wide: true,
      })}

      <div class="min-w-0 sm:col-span-2">
        <label class="stamp block" for="mtls-pem">Paste a PEM certificate to read its fingerprint</label>
        <textarea
          id="mtls-pem"
          class="exhibit mt-1.5"
          rows={5}
          spellcheck="false"
          placeholder={"-----BEGIN CERTIFICATE-----\n..."}
          bind:value={mtlsCertPEM}
        ></textarea>
        {#if mtlsCertError}
          <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-seal">
            {mtlsCertError} — paste the whole PEM block, including the BEGIN and END lines.
          </p>
        {/if}
        <button
          type="button"
          class="act mt-3"
          disabled={!mtlsCertPEM.trim()}
          onclick={() => void useMTLSCertificateFingerprint()}
        >
          Use cert fingerprint
        </button>
      </div>
    </div>
  </Section>

  <Section title="Standing access" note="Grants that do not expire.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Role IDs",
        value: editor.getList("role_ids"),
        set: (value) => editor.setList("role_ids", value),
        placeholder: "service-role",
        mono: true,
      })}
      {@render line({
        label: "Sync role IDs",
        value: editor.getList("sync_role_ids"),
        set: (value) => editor.setList("sync_role_ids", value),
        placeholder: "ldap-service-role",
        mono: true,
        hint: "Written by LDAP sync; edits here are overwritten on the next sync.",
      })}
      {@render line({
        label: "Permission IDs",
        value: editor.getList("permission_ids"),
        set: (value) => editor.setList("permission_ids", value),
        placeholder: "service-read",
        mono: true,
      })}
      {@render toggle({
        label: "Active",
        on: editor.getBool("is_active", true),
        set: (value) => editor.setBool("is_active", value),
        hint: "Turning this off stops every token issued for this account on the next request.",
      })}
    </div>
  </Section>
{:else if editor.kind === "roles"}
  <Section title="Role" note="Roles bundle permissions and can contain other roles.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Role name",
        value: editor.getString("name"),
        set: (value) => editor.setString("name", value),
        placeholder: "my-role",
      })}
      {@render line({
        label: "Description",
        value: editor.getString("description"),
        set: (value) => editor.setString("description", value),
        placeholder: "optional",
      })}
      {@render line({
        label: "Permission IDs",
        value: editor.getList("permission_ids"),
        set: (value) => editor.setList("permission_ids", value),
        placeholder: "read-api, write-api",
        mono: true,
      })}
      {@render line({
        label: "Included role IDs",
        value: editor.getList("role_ids"),
        set: (value) => editor.setList("role_ids", value),
        placeholder: "base-role",
        mono: true,
        hint: "Everything those roles carry is carried by this one.",
      })}
      {@render exhibit({
        label: "Data JSON",
        value: editor.getJSON("data"),
        set: (value) => editor.setJSON("data", value),
        lazy: true,
        rows: 6,
        placeholder: '{\n  "tenant": "default"\n}',
        hint: "A JSON object stored as role.data.",
      })}
    </div>
  </Section>
{:else if editor.kind === "permissions"}
  <Section title="Permission" note="What this permission is called.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "Permission name",
        value: editor.getString("name"),
        set: (value) => editor.setString("name", value),
        placeholder: "my-permission",
      })}
      {@render line({
        label: "Description",
        value: editor.getString("description"),
        set: (value) => editor.setString("description", value),
        placeholder: "optional",
      })}
    </div>

	<div class="mt-6 border-t border-rule pt-5">
	  {@render toggle({
	    label: "Make these resources public",
	    on: editor.getBool("public"),
	    set: (value) => editor.setBool("public", value),
	    consequential: true,
	    hint: "Anonymous requests and every signed-in user may access matching resources. Host, path, method and excluded-resource rules below still apply.",
	  })}
	</div>
  </Section>

  <Section
    title="Resources"
    note="Host, path and method patterns this permission matches. A permission with no resources matches nothing."
  >
    {#snippet aside()}
      <button type="button" class="act" onclick={() => editor.addResource()}>Add resource</button>
    {/snippet}

    {#if resources.length === 0}
      <p class="border border-dashed border-rule px-6 py-10 text-center text-[13px] text-muted">
        No resources yet — this permission grants nothing until one is added.
      </p>
    {:else}
      <ul class="border border-rule bg-sheet">
        {#each resources as _, i (i)}
          {@const exclusions = editor.excludedResources(i)}
          <li class="border-b border-rule last:border-b-0">
            <div class="flex items-center justify-between gap-4 border-b border-rule px-4 py-2">
              <span class="stamp stamp-ink">Resource {String(i + 1).padStart(2, "0")}</span>
              <button
                type="button"
                class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                onclick={() => editor.removeResource(i)}
              >
                Remove
              </button>
            </div>
            <div class="grid gap-6 px-4 py-4 sm:grid-cols-3">
              <div class="min-w-0">
                <label class="stamp block" for="resource-hosts-{i}">Hosts</label>
                <input
                  id="resource-hosts-{i}"
                  class="entry serial mt-1.5"
                  placeholder="api.example.com"
                  autocomplete="off"
                  value={editor.getResourceList(i, "hosts")}
                  oninput={(event) => editor.setResourceList(i, "hosts", event.currentTarget.value)}
                />
              </div>
              <div class="min-w-0">
                <label class="stamp block" for="resource-paths-{i}">Paths</label>
                <input
                  id="resource-paths-{i}"
                  class="entry serial mt-1.5"
                  placeholder="/api/**"
                  autocomplete="off"
                  value={editor.getResourceList(i, "paths")}
                  oninput={(event) => editor.setResourceList(i, "paths", event.currentTarget.value)}
                />
              </div>
              <div class="min-w-0">
                <label class="stamp block" for="resource-methods-{i}">Methods</label>
                <input
                  id="resource-methods-{i}"
                  class="entry serial mt-1.5"
                  placeholder="GET, POST"
                  autocomplete="off"
                  value={editor.getResourceList(i, "methods")}
                  oninput={(event) => editor.setResourceList(i, "methods", event.currentTarget.value)}
                />
              </div>
            </div>

            <div class="border-t border-rule bg-raised/35 px-4 py-4">
              <div class="flex flex-wrap items-start justify-between gap-3">
                <div>
                  <p class="stamp stamp-ink">Excluded resources</p>
                  <p class="mt-1 max-w-[68ch] text-[12px] leading-[1.5] text-muted">
                    Matching exclusions veto this resource. Use <span class="serial">/**</span> and
                    <span class="serial">*</span> to exclude every path and method for a host.
                  </p>
                </div>
                <button type="button" class="act" onclick={() => editor.addExcludedResource(i)}>
                  Add exclusion
                </button>
              </div>
              {#if exclusions.length === 0}
                <p class="mt-4 border border-dashed border-rule px-4 py-5 text-center text-[12px] text-muted">
                  No exclusions. Every request matching the resource above is allowed by this permission.
                </p>
              {:else}
                <ul class="mt-4 border border-rule bg-sheet">
                  {#each exclusions as _, j (j)}
                    <li class="border-b border-rule last:border-b-0">
                      <div class="flex items-center justify-between gap-4 border-b border-rule px-3 py-2">
                        <span class="stamp">Exclusion {String(j + 1).padStart(2, "0")}</span>
                        <button
                          type="button"
                          class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                          onclick={() => editor.removeExcludedResource(i, j)}
                        >
                          Remove
                        </button>
                      </div>
                      <div class="grid gap-5 px-3 py-4 sm:grid-cols-3">
                        <div class="min-w-0">
                          <label class="stamp block" for="resource-{i}-excluded-{j}-hosts">Hosts</label>
                          <input
                            id="resource-{i}-excluded-{j}-hosts"
                            class="entry serial mt-1.5"
                            placeholder="internal.example.com"
                            autocomplete="off"
                            value={editor.getExcludedResourceList(i, j, "hosts")}
                            oninput={(event) =>
                              editor.setExcludedResourceList(i, j, "hosts", event.currentTarget.value)}
                          />
                        </div>
                        <div class="min-w-0">
                          <label class="stamp block" for="resource-{i}-excluded-{j}-paths">Paths</label>
                          <input
                            id="resource-{i}-excluded-{j}-paths"
                            class="entry serial mt-1.5"
                            placeholder="/**"
                            autocomplete="off"
                            value={editor.getExcludedResourceList(i, j, "paths")}
                            oninput={(event) =>
                              editor.setExcludedResourceList(i, j, "paths", event.currentTarget.value)}
                          />
                        </div>
                        <div class="min-w-0">
                          <label class="stamp block" for="resource-{i}-excluded-{j}-methods">Methods</label>
                          <input
                            id="resource-{i}-excluded-{j}-methods"
                            class="entry serial mt-1.5"
                            placeholder="*"
                            autocomplete="off"
                            value={editor.getExcludedResourceList(i, j, "methods")}
                            oninput={(event) =>
                              editor.setExcludedResourceList(i, j, "methods", event.currentTarget.value)}
                          />
                        </div>
                      </div>
                    </li>
                  {/each}
                </ul>
              {/if}
            </div>
          </li>
        {/each}
      </ul>
    {/if}
  </Section>

  <Section title="Carried data" note="Permission metadata that is passed along with an allowed request.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render exhibit({
        label: "Data JSON",
        value: editor.getJSON("data"),
        set: (value) => editor.setJSON("data", value),
        lazy: true,
        rows: 6,
        placeholder: '{\n  "tenant": "default",\n  "region": "eu"\n}',
        hint: "A JSON object stored as permission.data.",
      })}
      {@render exhibit({
        label: "Scope role map JSON",
        value: editor.getJSON("scope"),
        set: (value) => editor.setJSON("scope", value),
        lazy: true,
        rows: 6,
        placeholder: '{\n  "openid": ["role-id-1", "role-id-2"],\n  "admin": ["admin-role-id"]\n}',
        hint: "Each key is a scope; each value is an array of role IDs.",
      })}
    </div>
  </Section>
{:else if editor.kind === "lmaps"}
  <Section title="Group map" note="Members of this LDAP group receive the mapped roles as sync roles.">
    <div class="grid gap-6 sm:grid-cols-2">
      {@render line({
        label: "LDAP group name",
        value: editor.getString("name"),
        set: (value) => editor.setString("name", value),
        placeholder: "ldap-group",
        mono: true,
      })}
      {@render line({
        label: "Role IDs",
        value: editor.getList("role_ids"),
        set: (value) => editor.setList("role_ids", value),
        placeholder: "admin, operator",
        mono: true,
      })}
    </div>
  </Section>
{/if}

{#if editor.kind === "users" || editor.kind === "service-accounts"}
  <TemporaryAccessPanel />
{/if}

{#if editor.kind === "users" && editor.loadedID}
  {#key editor.loadedID}
    <PasskeyPanel userID={editor.loadedID} />
  {/key}
{/if}
