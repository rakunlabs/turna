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

  const disabled = $derived(getSettingBool("totp", ["disabled"]));
  const issuer = $derived(getSettingString("totp", ["issuer"]));
  const skew = $derived(getSettingNumber("totp", ["skew"], 1));

  /** One period is 30 seconds either side, so the tolerated drift is legible. */
  const drift = $derived(skew <= 0 ? "no drift tolerated" : `±${skew * 30}s of clock drift`);
</script>

<Instrument
  title="TOTP"
  note="Time-based one-time codes (RFC 6238) as a second factor on password logins. Secrets are sealed at rest and enrolled per user."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("totp")}
    >
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">totp</span></span>
    <Seal
      state={disabled ? "void" : "endorsed"}
      label={disabled ? "Not verified" : "Verified at login"}
    />
  {/snippet}

  <Section title="Standing" first>
    <Switch
      label="Stop asking for authenticator codes"
      consequential
      hint="On, enrolled users sign in with their password alone and their existing secrets are ignored — a login that used to need a second factor is now accepted without one. Nothing is deleted, so turning this back off restores every enrolment."
      bind:checked={
        () => getSettingBool("totp", ["disabled"]),
        (value: boolean) => setSettingBool("totp", ["disabled"], value)
      }
    />
  </Section>

  <Section title="Enrolment" note="How this instance names itself inside an authenticator app.">
    <div class="grid max-w-[52ch] gap-6">
      <div>
        <label class="stamp block" for="totp-issuer">Issuer</label>
        <input
          id="totp-issuer"
          class="entry mt-1.5"
          autocomplete="off"
          placeholder="Turna Auth"
          aria-describedby="totp-issuer-hint"
          value={issuer}
          oninput={(e) => setSettingString("totp", ["issuer"], e.currentTarget.value)}
        />
        <p id="totp-issuer-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Shown above the code in the user's app. Changing it does not invalidate anything already
          enrolled — existing entries keep the name they were scanned with.
        </p>
      </div>

      <div>
        <label class="stamp block" for="totp-skew">Skew periods</label>
        <input
          id="totp-skew"
          class="entry serial mt-1.5"
          type="number"
          min="0"
          aria-describedby="totp-skew-hint"
          value={skew}
          oninput={(e) => setSettingNumber("totp", ["skew"], e.currentTarget.value)}
        />
        <p id="totp-skew-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Adjacent 30-second periods also accepted, for phones whose clock has drifted. Each extra
          period widens the window a valid code stays usable in.
        </p>
      </div>
    </div>
  </Section>

  <Section title="Accepted window">
    <div class="flex flex-wrap items-end gap-x-10 gap-y-4">
      <div>
        <Serial value={drift} size="md" tone={skew > 2 ? "seal" : "ink"} />
        <p class="stamp mt-2">Tolerance</p>
      </div>
      <p class="max-w-[52ch] flex-1 basis-72 text-[13px] leading-[1.6] text-muted">
        A code is valid for its own 30-second period plus {skew}
        {skew === 1 ? "period" : "periods"} either side. One or two is normal; larger values leave a
        stolen code usable for longer.
      </p>
    </div>
  </Section>
</Instrument>
