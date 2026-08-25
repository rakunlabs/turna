<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Seal from "./ui/Seal.svelte";
  import { registry } from "../lib/state/registry.svelte";
  import { route } from "../lib/state/route.svelte";
  import { getSettingBool } from "../lib/state/settings.svelte";
  import { hrefOf, labelOf, plainClick, type Tab } from "../lib/navigation";

  /**
   * A standing report, not a control panel. Every line reads live settings and
   * says what is true right now; the button hands the operator to the page that
   * owns the switch. Nothing on this page writes anything.
   */
  type Flow = { label: string; on: boolean; meaning: string; tab: Tab };

  const ldapReachable = $derived(registry.ldapActive && !getSettingBool("password", ["ldap_disabled"]));

  const waysIn: Flow[] = $derived([
    {
      label: "Password grant",
      on: !getSettingBool("password", ["disabled"]),
      meaning:
        "A registered client may exchange a username and password for a token at the token endpoint.",
      tab: "oauth2-overview",
    },
    {
      label: "Local passwords",
      on: !getSettingBool("password", ["local_disabled"]),
      meaning: "Users stored in this instance verify against their own bcrypt password.",
      tab: "oauth2-overview",
    },
    {
      label: "LDAP passwords",
      on: ldapReachable,
      meaning: "Users that are not local bind against the first enabled LDAP config instead.",
      tab: "ldap",
    },
    {
      label: "LDAP auto-register",
      on: ldapReachable && !getSettingBool("password", ["ldap_register_disabled"]),
      meaning:
        "An alias found in LDAP but not yet stored here is created as a non-local user on first login.",
      tab: "ldap",
    },
    {
      label: "Passkey / WebAuthn",
      on: !getSettingBool("passkey", ["disabled"]),
      meaning: "Enrolled authenticators sign a challenge instead of sending a password.",
      tab: "oauth2-overview",
    },
    {
      label: "Self registration",
      on: getSettingBool("signup", ["enabled"]),
      meaning: "Anyone reaching the login page may create a local account for themselves.",
      tab: "signup",
    },
    {
      label: "mTLS client certificates",
      on: getSettingBool("mtls", ["enabled"]),
      meaning: "A client certificate presented at the token endpoint authenticates the caller.",
      tab: "mtls",
    },
  ]);

  const proofs: Flow[] = $derived([
    {
      label: "TOTP second factor",
      on: !getSettingBool("totp", ["disabled"]),
      meaning: "A user who enrolled TOTP must supply a valid code before a login completes.",
      tab: "totp",
    },
    {
      label: "Signup email verification",
      on: getSettingBool("signup", ["email_verification"], true),
      meaning: "A new account is created only after the address answers a mailed code or link.",
      tab: "signup",
    },
    {
      label: "Password reset by email",
      on: getSettingBool("signup", ["password_reset"]),
      meaning: "A user who lost their password can set a new one through a mailed link.",
      tab: "signup",
    },
  ]);

  const grants: Flow[] = $derived([
    {
      label: "API keys",
      on: !getSettingBool("api_key", ["disabled"]),
      meaning: "A static key is accepted as a credential and checked against the database per request.",
      tab: "api-keys",
    },
    {
      label: "Device flow",
      on: !getSettingBool("device", ["disabled"]),
      meaning: "An input-limited device gets a user code and polls until a person approves it.",
      tab: "device-settings",
    },
    {
      label: "Token exchange",
      on: !getSettingBool("token_exchange", ["disabled"]),
      meaning: "A held token can be traded for another one under the exchange grant.",
      tab: "token-exchange",
    },
  ]);

  const federation: Flow[] = $derived([
    {
      label: "Upstream OAuth providers",
      on: registry.providerCount > 0,
      meaning:
        "Login can be delegated to an upstream identity provider, which returns a code this instance redeems.",
      tab: "providers",
    },
    {
      label: "SAML providers",
      on: registry.samlCount > 0,
      meaning: "A SAML assertion is accepted and converted into a standard authorization code.",
      tab: "saml",
    },
    {
      label: "LDAP directory",
      on: registry.ldapActive,
      meaning: "A directory is connected for password binds and scheduled group synchronisation.",
      tab: "ldap",
    },
  ]);

  const sealed = $derived(
    waysIn.every((flow) => !flow.on) && federation.every((flow) => !flow.on),
  );

  const onCount = $derived(
    [...waysIn, ...proofs, ...grants, ...federation].filter((flow) => flow.on).length,
  );
</script>

{#snippet ledger(flows: Flow[])}
  <ul>
    {#each flows as flow (flow.label)}
      <li
        class="grid gap-x-6 gap-y-2 border-b border-rule py-3.5 last:border-b-0 md:grid-cols-[minmax(0,14rem)_minmax(0,1fr)_auto] md:items-baseline"
      >
        <span class="flex min-w-0 items-center gap-2.5">
          <Seal state={flow.on ? "endorsed" : "void"} />
          <span class="min-w-0 text-[13.5px] font-medium text-ink">{flow.label}</span>
        </span>

        <span class="min-w-0 text-[12.5px] leading-[1.55] text-muted">
          {flow.meaning}
        </span>

        <span class="flex shrink-0 items-center gap-4 md:justify-end">
          <span class="stamp {flow.on ? 'text-endorsed' : 'text-muted'}">{flow.on ? "On" : "Off"}</span>
          <a
            href={hrefOf(flow.tab)}
            class="act no-underline"
            onclick={(event) => {
              if (!plainClick(event)) return;
              event.preventDefault();
              route.select(flow.tab);
            }}
          >
            Open {labelOf(flow.tab)}
          </a>
        </span>
      </li>
    {/each}
  </ul>
{/snippet}

{#snippet count(flows: Flow[])}
  <span class="stamp">{flows.filter((flow) => flow.on).length} of {flows.length} on</span>
{/snippet}

<Instrument
  title="Flows"
  note="What this instance currently accepts. Every line reads the live settings — this is a report, not a set of switches, so each one hands you to the page that owns it."
>
  {#snippet custody()}
    <span class="stamp">{onCount} enabled</span>
    <span class="stamp">Read from the live settings namespaces</span>
  {/snippet}

  {#if sealed}
    <p class="hatch mb-8 border border-seal/45 px-4 py-3 text-[13px] leading-[1.55] text-ink">
      <span class="stamp text-seal">Sealed shut</span>
      <span class="ml-3">
        No way in is enabled and no upstream is connected, so nothing can obtain a token from this
        instance. Enable a login method below before anyone depends on it.
      </span>
    </p>
  {/if}

  <Section title="Ways in" note="How a person or a machine first proves who it is." first>
    {#snippet aside()}{@render count(waysIn)}{/snippet}
    {@render ledger(waysIn)}
  </Section>

  <Section title="Additional proof" note="Steps that stand between a correct password and a finished login.">
    {#snippet aside()}{@render count(proofs)}{/snippet}
    {@render ledger(proofs)}
  </Section>

  <Section title="Grants and credentials" note="Ways a token is obtained or traded once an identity exists.">
    {#snippet aside()}{@render count(grants)}{/snippet}
    {@render ledger(grants)}
  </Section>

  <Section title="Federation" note="Identity sources outside this instance. These count records, not settings.">
    {#snippet aside()}{@render count(federation)}{/snippet}
    {@render ledger(federation)}
  </Section>
</Instrument>
