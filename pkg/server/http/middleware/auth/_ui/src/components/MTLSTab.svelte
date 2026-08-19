<script lang="ts">
  import type { Tab } from "../lib/navigation";

  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import { session } from "../lib/state/session.svelte";
  import {
    getSettingBool,
    setSettingBool,
    getSettingString,
    setSettingString,
    saveSetting,
  } from "../lib/state/settings.svelte";

  let { onselect }: { onselect: (tab: Tab) => void } = $props();

  const enabled = $derived(getSettingBool("mtls", ["enabled"]));
  const certHeader = $derived(getSettingString("mtls", ["cert_header"]));

  /**
   * A header is only a claim about a certificate: whoever can reach this
   * instance directly can set it. Naming that standing on the page is the whole
   * point of this control, so it is derived rather than left to the reader.
   */
  const trustingHeader = $derived(enabled && certHeader.trim() !== "");
</script>

<Instrument
  title="mTLS"
  note="Client certificates authenticate service accounts on the OAuth2 client_credentials grant. The token endpoint matches the presented certificate against that service account's fingerprint or subject."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("mtls")}
    >
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">mtls</span></span>
    <Seal
      state={enabled ? "endorsed" : "void"}
      label={enabled ? "Certificates accepted" : "Not accepted"}
    />
  {/snippet}

  <Section title="Acceptance" first>
    <div class="grid gap-6">
      <Switch
        label="Accept client certificates at the token endpoint"
        consequential
        hint="On, a service account can obtain a token by presenting its certificate instead of a client secret. This widens what the token endpoint accepts — every certificate binding on every service account becomes a live credential."
        bind:checked={
          () => getSettingBool("mtls", ["enabled"]),
          (value: boolean) => setSettingBool("mtls", ["enabled"], value)
        }
      />

      <div class="max-w-[52ch]">
        <label class="stamp block" for="mtls-cert-header">Trusted certificate header</label>
        <input
          id="mtls-cert-header"
          class="entry serial mt-1.5"
          autocomplete="off"
          spellcheck="false"
          placeholder="ssl-client-cert"
          aria-describedby="mtls-cert-header-hint"
          value={certHeader}
          oninput={(e) => setSettingString("mtls", ["cert_header"], e.currentTarget.value)}
        />
        <p id="mtls-cert-header-hint" class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
          Empty means the certificate is taken from the TLS handshake itself, which cannot be forged.
          Set this only when a TLS-terminating proxy is the single way in.
        </p>
      </div>
    </div>

    {#if trustingHeader}
      <p class="hatch mt-6 max-w-[70ch] border border-seal/40 px-4 py-3 text-[13px] leading-[1.55] text-ink">
        <span class="stamp text-seal">Bypass risk</span>
        <span class="mt-1.5 block">
          This instance now believes <span class="serial">{certHeader}</span> on every request. If
          anything can reach it without passing through the proxy that sets that header, that caller
          can name any certificate it likes and authenticate as any service account. The proxy must
          strip and rewrite the header on the way in, and this route must not be reachable around it.
        </span>
      </p>
    {/if}
  </Section>

  <Section
    title="Where client certificates live"
    note="There is no certificate list here. Each mTLS client is a service account, and its binding is stored on that record."
  >
    <ul class="max-w-[70ch]">
      <li class="border-b border-rule py-3 text-[13px] leading-[1.6] text-ink">
        Fill either <span class="serial">cert_fingerprint</span> or
        <span class="serial">cert_subject</span> in the certificate section of the service account.
      </li>
      <li class="border-b border-rule py-3 text-[13px] leading-[1.6] text-ink">
        The service account alias is the OAuth2 <span class="serial">client_id</span>.
      </li>
      <li class="py-3 text-[13px] leading-[1.6] text-ink">
        For a certificate-only client the <span class="serial">client_secret</span> may be left empty.
      </li>
    </ul>

    <button type="button" class="act mt-5" onclick={() => onselect("service-accounts")}>
      Open service accounts
    </button>
  </Section>

  <Section title="Token request">
    <pre class="exhibit overflow-auto">curl --cert client.crt --key client.key \
  -X POST {session.oauthBase}/oauth2/token \
  -d grant_type=client_credentials \
  -d client_id=my-service</pre>
  </Section>

  <Section
    title="Session integration"
    note="Session never inspects the raw certificate. Auth validates the certificate and issues a normal access token; session then validates that token through auth_middleware or JWKS and forwards the claims and X-User to the application."
  >
    <pre class="exhibit overflow-auto">curl https://app.example.com/api \
  -H 'Authorization: Bearer &lt;access_token&gt;'</pre>
  </Section>
</Instrument>
