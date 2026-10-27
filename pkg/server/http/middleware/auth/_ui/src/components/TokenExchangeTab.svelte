<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import ListEntry from "./ui/ListEntry.svelte";
  import { session } from "../lib/state/session.svelte";
  import {
    getSettingBool,
    setSettingBool,
    getSettingValue,
    setSettingValue,
    saveSetting,
  } from "../lib/state/settings.svelte";

  const disabled = $derived(getSettingBool("token_exchange", ["disabled"]));
</script>

<Instrument
  title="Token exchange"
  note="The RFC 8693 grant, which lets a client hand in one token and receive another at the token endpoint. Used to step a token down for a downstream service, or to act on behalf of the subject that presented it."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("token_exchange")}
    >
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">token_exchange</span></span>
    <Seal state={disabled ? "void" : "endorsed"} label={disabled ? "Not accepted" : "Accepted"} />
  {/snippet}

  <Section title="Standing" first>
    <Switch
      label="Refuse the token exchange grant"
      hint="On, the token endpoint answers unsupported_grant_type for every exchange request. Tokens already issued through an exchange keep working until they expire — this only stops new ones."
      bind:checked={
        () => getSettingBool("token_exchange", ["disabled"]),
        (value: boolean) => setSettingBool("token_exchange", ["disabled"], value)
      }
    />

    <div class="mt-6 max-w-[62ch]">
      <label class="stamp block" for="token-exchange-allowed-clients">Clients allowed to exchange others' tokens</label>
      <ListEntry
        id="token-exchange-allowed-clients"
        placeholder="api-gateway, reporting-service"
        describedby="token-exchange-allowed-clients-hint"
        value={getSettingValue("token_exchange", ["allowed_clients"])}
        onchange={(items) => setSettingValue("token_exchange", ["allowed_clients"], items)}
      />
      <p id="token-exchange-allowed-clients-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
        A client can always exchange a token that was issued to itself. Listing client IDs here
        restricts exchanging a token issued to a different client to just these; left empty, any
        confidential client may.
      </p>
    </div>
  </Section>

  <Section title="The grant" note="What a client sends to {session.oauthIssuer}/token.">
    <pre class="exhibit overflow-auto">grant_type=urn:ietf:params:oauth:grant-type:token-exchange
subject_token=&lt;the token being handed in&gt;
subject_token_type=urn:ietf:params:oauth:token-type:access_token</pre>

    <p class="mt-5 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
      The subject token is validated the same way any other token is. The client presenting it still
      authenticates as itself. The exchanged token can only narrow the subject token's scope, stays
      inside its session — revoking that session revokes it, and it never outlives it — and comes
      without a refresh token.
    </p>
  </Section>
</Instrument>
