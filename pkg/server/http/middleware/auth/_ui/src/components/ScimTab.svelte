<script lang="ts">
  import { untrack } from "svelte";
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import Serial from "./ui/Serial.svelte";
  import { docket, session } from "../lib/state/session.svelte";
  import {
    settings,
    getSettingBool,
    setSettingBool,
    setSettingString,
    saveSetting,
  } from "../lib/state/settings.svelte";

  const enabled = $derived(getSettingBool("scim", ["enabled"]));
  const tokenSet = $derived(getSettingBool("scim", ["bearer_token_set"]));
  const baseURL = $derived(`${session.oauthIssuer.replace(/\/oauth2$/, "")}/scim/v2`);

  let issued = $state("");

  /** A fresh token is shown once; it is write-only once committed. */
  async function issueToken() {
    const bytes = crypto.getRandomValues(new Uint8Array(32));
    issued = Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
    setSettingString("scim", ["bearer_token"], issued);
    await saveSetting("scim");
  }

  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      docket.commit("Copied to the clipboard");
    } catch {
      docket.reject("Copy failed — select the value and copy it by hand.");
    }
  }

  $effect(() => {
    untrack(() => void settings.load("scim"));
  });
</script>

<Instrument
  title="SCIM"
  note="SCIM 2.0 provisioning for Okta, Microsoft Entra ID and other identity providers. Users are created, updated and disabled by the IdP; groups become roles and their members hold the role."
>
  {#snippet actions()}
    <button type="button" class="act act-primary" disabled={session.busy} onclick={() => saveSetting("scim")}>Commit</button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">scim</span></span>
    <Seal state={enabled && tokenSet ? "endorsed" : "void"} label={enabled && tokenSet ? "Accepting provisioning" : "Off"} />
  {/snippet}

  <Section title="Standing" first>
    <Switch
      label="Accept SCIM provisioning"
      consequential
      hint="The IdP can create, change, disable and delete users and change role membership. Every change is recorded in the audit log with actor scim."
      bind:checked={() => getSettingBool("scim", ["enabled"]), (value: boolean) => setSettingBool("scim", ["enabled"], value)}
    />
    <div class="mt-5">
      <Switch
        label="Disable instead of delete"
        hint="A DELETE from the IdP disables the user and keeps the record, its audit trail and its API keys."
        bind:checked={() => getSettingBool("scim", ["delete_disables"]), (value: boolean) => setSettingBool("scim", ["delete_disables"], value)}
      />
    </div>
  </Section>

  <Section title="Connection" note="Enter these in the IdP's SCIM provisioning settings (Okta: SCIM connector base URL + HTTP Header; Entra ID: Tenant URL + Secret Token).">
    <div class="grid max-w-[70ch] gap-6">
      <div>
        <p class="stamp">Base URL</p>
        <div class="mt-1.5 flex items-center gap-3">
          <Serial value={baseURL} size="md" />
          <button type="button" class="act act-quiet" onclick={() => copy(baseURL)}>Copy</button>
        </div>
      </div>
      <div>
        <p class="stamp">Bearer token</p>
        {#if issued}
          <div class="mt-1.5 flex items-center gap-3">
            <Serial value={issued} size="md" tone="seal" />
            <button type="button" class="act act-quiet" onclick={() => copy(issued)}>Copy</button>
          </div>
          <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">Shown once. The previous token stopped working.</p>
        {:else}
          <p class="mt-1.5 text-[13px] text-muted">{tokenSet ? "A token is set." : "No token yet."}</p>
        {/if}
        <button type="button" class="act mt-3" disabled={session.busy} onclick={() => void issueToken()}>
          {tokenSet ? "Replace token" : "Generate token"}
        </button>
      </div>
    </div>
  </Section>
</Instrument>
