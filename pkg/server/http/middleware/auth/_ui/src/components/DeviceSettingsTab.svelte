<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import Serial from "./ui/Serial.svelte";
  import { session } from "../lib/state/session.svelte";
  import {
    getSettingBool,
    setSettingBool,
    getSettingString,
    setSettingString,
    getSettingNumber,
    setSettingNumber,
    saveSetting,
  } from "../lib/state/settings.svelte";

  const disabled = $derived(getSettingBool("device", ["disabled"]));
  const codeLifetime = $derived(getSettingString("device", ["code_lifetime"]));
  const interval = $derived(getSettingNumber("device", ["interval"], 5));
  const verificationURI = $derived(getSettingString("device", ["verification_uri"]));

  /** What a device is actually told to display when the field is left empty. */
  const effectiveURI = $derived(verificationURI.trim() || `${session.oauthBase}/ui/device`);
</script>

<Instrument
  title="Device flow"
  note="The RFC 8628 device authorization grant, for clients that cannot open a browser. The device shows a code, the person approves it here, and the device polls the token endpoint until it is answered."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("device")}
    >
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">device</span></span>
    <Seal state={disabled ? "void" : "endorsed"} label={disabled ? "Not issued" : "Issuing codes"} />
  {/snippet}

  <Section title="Standing" first>
    <Switch
      label="Stop issuing device codes"
      hint="On, /oauth2/device rejects new requests. Devices already holding a code stop being answered and their users see nothing to approve."
      bind:checked={
        () => getSettingBool("device", ["disabled"]),
        (value: boolean) => setSettingBool("device", ["disabled"], value)
      }
    />
  </Section>

  <Section title="Timing" note="How long a code stands, and how often a device may ask about it.">
    <div class="grid max-w-[52ch] gap-6 sm:grid-cols-2">
      <div>
        <label class="stamp block" for="device-code-lifetime">Code lifetime</label>
        <input
          id="device-code-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          placeholder="10m"
          aria-describedby="device-code-lifetime-hint"
          value={codeLifetime}
          oninput={(e) => setSettingString("device", ["code_lifetime"], e.currentTarget.value)}
        />
        <p id="device-code-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          A Go duration. After this the code expires and the device must start again.
        </p>
      </div>

      <div>
        <label class="stamp block" for="device-interval">Poll interval</label>
        <input
          id="device-interval"
          class="entry serial mt-1.5"
          type="number"
          min="1"
          aria-describedby="device-interval-hint"
          value={interval}
          oninput={(e) => setSettingNumber("device", ["interval"], e.currentTarget.value)}
        />
        <p id="device-interval-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Seconds the device is told to wait between polls. Asking faster is answered with
          <span class="serial">slow_down</span>.
        </p>
      </div>
    </div>
  </Section>

  <Section
    title="Where people approve"
    note="This address is printed on the device screen, so it has to be reachable and readable by whoever is holding it."
  >
    <div class="max-w-[62ch]">
      <label class="stamp block" for="device-verification-uri">Verification URI</label>
      <input
        id="device-verification-uri"
        class="entry serial mt-1.5"
        autocomplete="off"
        spellcheck="false"
        placeholder="{session.oauthBase}/ui/device"
        aria-describedby="device-verification-uri-hint"
        value={verificationURI}
        oninput={(e) => setSettingString("device", ["verification_uri"], e.currentTarget.value)}
      />
      <p id="device-verification-uri-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
        Leave empty to use this console's own approval page. Set it when the console is behind a
        different public address than the one this instance sees.
      </p>
    </div>

    <div class="mt-8">
      <Serial value={effectiveURI} size="md" tone="carbon" />
      <p class="stamp mt-2">Printed on the device</p>
    </div>
  </Section>
</Instrument>
