<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import { session } from "../lib/state/session.svelte";
  import { getSettingBool, setSettingBool, saveSetting } from "../lib/state/settings.svelte";

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
  </Section>

  <Section title="The grant" note="What a client sends to {session.oauthBase}/oauth2/token.">
    <pre class="exhibit overflow-auto">grant_type=urn:ietf:params:oauth:grant-type:token-exchange
subject_token=&lt;the token being handed in&gt;
subject_token_type=urn:ietf:params:oauth:token-type:access_token</pre>

    <p class="mt-5 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
      The subject token is validated the same way any other token is. The client presenting it still
      authenticates as itself, so this widens what a client can obtain, never who it is.
    </p>
  </Section>
</Instrument>
