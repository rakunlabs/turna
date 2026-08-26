<script lang="ts">
  import { hrefOf, plainClick, type Tab } from "../lib/navigation";

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
    getSettingList,
    setSettingList,
    saveSetting,
  } from "../lib/state/settings.svelte";

  let { onselect }: { onselect: (tab: Tab) => void } = $props();

  const enabled = $derived(getSettingBool("mtls", ["enabled"]));
  const certHeader = $derived(getSettingString("mtls", ["cert_header"]));
  const certVerifyHeader = $derived(getSettingString("mtls", ["cert_verify_header"]));
  const certVerifyValue = $derived(getSettingString("mtls", ["cert_verify_value"]) || "SUCCESS");
  const trustedProxyCIDRs = $derived(getSettingList("mtls", ["trusted_proxy_cidrs"]));

  /**
   * A header is only a claim about a certificate: whoever can reach this
   * instance directly can set it. Naming that standing on the page is the whole
   * point of this control, so it is derived rather than left to the reader.
   */
  const headerConfigured = $derived(enabled && certHeader.trim() !== "");
  const proxyReady = $derived(
    headerConfigured && certVerifyHeader.trim() !== "" && trustedProxyCIDRs.trim() !== "",
  );
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
      state={enabled ? (headerConfigured && !proxyReady ? "held" : "endorsed") : "void"}
      label={enabled ? (headerConfigured && !proxyReady ? "Proxy trust incomplete" : "Certificates accepted") : "Not accepted"}
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

      <div class="max-w-[62ch]">
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
          Empty selects native TLS. Turna then accepts only a chain verified by
          <span class="serial">server.http.tls.client_ca_files</span>. Set a header only when a
          TLS-terminating proxy is the single way in.
        </p>
      </div>

      {#if headerConfigured}
        <div class="grid gap-6 sm:grid-cols-2">
          <div class="min-w-0">
            <label class="stamp block" for="mtls-verify-header">Verification result header</label>
            <input
              id="mtls-verify-header"
              class="entry serial mt-1.5"
              autocomplete="off"
              spellcheck="false"
              placeholder="ssl-client-verify"
              value={certVerifyHeader}
              oninput={(e) => setSettingString("mtls", ["cert_verify_header"], e.currentTarget.value)}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              The proxy must overwrite this with its client-certificate verification result.
            </p>
          </div>

          <div class="min-w-0">
            <label class="stamp block" for="mtls-verify-value">Successful result value</label>
            <input
              id="mtls-verify-value"
              class="entry serial mt-1.5"
              autocomplete="off"
              spellcheck="false"
              value={certVerifyValue}
              oninput={(e) => setSettingString("mtls", ["cert_verify_value"], e.currentTarget.value)}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              Defaults to <span class="serial">SUCCESS</span>, matching nginx
              <span class="serial">$ssl_client_verify</span>.
            </p>
          </div>

          <div class="min-w-0 sm:col-span-2">
            <label class="stamp block" for="mtls-trusted-proxies">Trusted proxy CIDRs</label>
            <textarea
              id="mtls-trusted-proxies"
              class="exhibit mt-1.5 min-h-24"
              spellcheck="false"
              placeholder={`192.0.2.10\n2001:db8:10::5`}
              value={trustedProxyCIDRs}
              oninput={(e) => setSettingList("mtls", ["trusted_proxy_cidrs"], e.currentTarget.value)}
            ></textarea>
            <p class="mt-1.5 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
              One immediate peer IP or narrowly dedicated CIDR per line. Forwarded address headers are deliberately ignored.
            </p>
          </div>
        </div>
      {/if}
    </div>

    {#if headerConfigured}
      <p class="hatch mt-6 max-w-[70ch] border border-seal/40 px-4 py-3 text-[13px] leading-[1.55] text-ink">
        <span class="stamp text-seal">Proxy trust boundary</span>
        <span class="mt-1.5 block">
          {#if proxyReady}
            Turna accepts <span class="serial">{certHeader}</span> only from the immediate peers above
            and only when <span class="serial">{certVerifyHeader}</span> equals
            <span class="serial">{certVerifyValue}</span>. The proxy must strip both incoming headers,
            verify the client chain and private-key proof, then write both values itself. Restrict the
            same peers with firewall or network policy.
          {:else}
            Header mode is incomplete and token requests will be rejected. Record a verification
            result header and at least one trusted immediate peer before committing this mode.
          {/if}
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

    <a
      href={hrefOf("service-accounts")}
      class="act mt-5 inline-block no-underline"
      onclick={(event) => {
        if (!plainClick(event)) return;
        event.preventDefault();
        onselect("service-accounts");
      }}
    >
      Open service accounts
    </a>
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
