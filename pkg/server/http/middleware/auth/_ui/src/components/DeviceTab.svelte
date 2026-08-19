<script lang="ts">
  import { onMount } from "svelte";

  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Seal from "./ui/Seal.svelte";
  import Serial from "./ui/Serial.svelte";
  import { docket, messageOf, session } from "../lib/state/session.svelte";
  import { route } from "../lib/state/route.svelte";

  type DeviceInfo = { client_id: string; scope: string; status: string };

  /**
   * The one page a person may ever see of this console: they were shown a code
   * on a television or a terminal and sent here to say yes or no. The code entry
   * owns the page, and the request is named in full before either choice.
   */
  let userCode = $state("");
  let info = $state<DeviceInfo | null>(null);
  let working = $state<"" | "lookup" | "approve" | "deny">("");
  let notFound = $state(false);
  let settled = $state<"" | "approve" | "deny">("");

  const digits = $derived(userCode.replace(/[\s-]+/g, ""));
  const complete = $derived(digits.length === 8);
  const busy = $derived(working !== "");

  const scopes = $derived(
    (info?.scope ?? "")
      .split(/[\s,]+/)
      .map((scope) => scope.trim())
      .filter(Boolean),
  );

  const pending = $derived(info?.status === "pending");

  function normalize(value: string) {
    const flat = value.toUpperCase().replace(/[\s-]+/g, "");
    return flat.length === 8 ? `${flat.slice(0, 4)}-${flat.slice(4)}` : value.toUpperCase();
  }

  async function lookup() {
    if (!complete || busy) return;

    working = "lookup";
    notFound = false;
    settled = "";
    info = null;

    try {
      const res = await session.request<DeviceInfo>(`device/${encodeURIComponent(normalize(userCode))}`);
      info = res.payload;
    } catch (err) {
      notFound = true;
      docket.reject(messageOf(err, "That code was not found"));
    } finally {
      working = "";
    }
  }

  async function decide(action: "approve" | "deny") {
    if (busy) return;

    working = action;

    try {
      await session.request("device", {
        method: "POST",
        body: JSON.stringify({ user_code: normalize(userCode), action }),
      });

      settled = action;
      info = null;
      userCode = "";
    } catch (err) {
      docket.reject(messageOf(err, "The device could not be answered"));
    } finally {
      working = "";
    }
  }

  function again() {
    settled = "";
    notFound = false;
    info = null;
    userCode = "";
  }

  onMount(() => {
    // Arriving from the device's own link: the code is already in the URL.
    if (route.deviceUserCode) {
      userCode = normalize(route.deviceUserCode);
      void lookup();
    }
  });
</script>

<Instrument
  title="Approve a device"
  note="A device — a terminal, a television, something without a keyboard worth using — showed you a short code and sent you here. Approving signs that device in as your account, with the access listed below."
>
  {#snippet custody()}
    <span class="stamp">Device authorization · RFC 8628</span>
    <span class="serial stamp-raw">{session.apiBase}/device</span>
  {/snippet}

  {#if settled}
    <div class="max-w-[70ch] border border-rule bg-sheet px-6 py-10">
      <Seal state={settled === "approve" ? "endorsed" : "void"} size={15} />
      <p class="mt-4 text-[1.35rem] font-bold leading-[1.2] tracking-[-0.02em] text-ink">
        {settled === "approve" ? "The device is signed in" : "The device was refused"}
      </p>
      <p class="mt-2.5 max-w-[56ch] text-[13.5px] leading-[1.6] text-muted">
        {settled === "approve"
          ? "Go back to the device — it should continue on its own within a few seconds. You can close this page."
          : "Nothing was granted and no token was issued. If you did not start this yourself, no further action is needed."}
      </p>
      <button type="button" class="act mt-7" onclick={again}>Enter another code</button>
    </div>
  {:else}
    <Section title="Your code" first>
      <div class="max-w-[26rem]">
        <label class="stamp block" for="device-user-code">The code shown on the device</label>
        <input
          id="device-user-code"
          class="entry serial mt-3 text-[2.25rem] leading-[1.2] tracking-[0.18em] uppercase"
          placeholder="XXXX-XXXX"
          maxlength="9"
          autocomplete="off"
          autocapitalize="characters"
          spellcheck="false"
          aria-describedby="device-user-code-hint"
          aria-invalid={notFound || undefined}
          style={notFound ? "border-bottom-color: var(--w-seal)" : undefined}
          bind:value={userCode}
          oninput={() => (notFound = false)}
          onkeydown={(e) => {
            if (e.key === "Enter") void lookup();
          }}
        />

        {#if notFound}
          <p id="device-user-code-hint" class="mt-2.5 text-[13px] leading-[1.55] text-seal">
            No pending request carries that code. Codes expire after a few minutes and each one can
            be used once — read it off the device again, or start the sign-in there a second time.
          </p>
        {:else}
          <p id="device-user-code-hint" class="mt-2.5 text-[13px] leading-[1.55] text-muted">
            Eight characters, with or without the dash. Nothing is granted by looking it up.
          </p>
        {/if}

        <button
          type="button"
          class="act act-primary mt-6"
          disabled={!complete || busy}
          onclick={() => void lookup()}
        >
          {working === "lookup" ? "Looking up…" : "Look up this code"}
        </button>
      </div>
    </Section>

    {#if info}
      <Section title="What is asking">
        {#snippet aside()}
          <Seal
            state={pending ? "held" : "void"}
            label={pending ? "Awaiting you" : "Already handled"}
          />
        {/snippet}

        <div class="max-w-[70ch]">
          <Serial value={info.client_id} size="md" />
          <p class="stamp mt-2">Client</p>

          <div class="mt-8">
            <p class="stamp">Access requested</p>
            {#if scopes.length === 0}
              <p class="mt-3 max-w-[62ch] text-[13px] leading-[1.6] text-muted">
                No scopes were named. The device receives a token that identifies you and nothing
                further.
              </p>
            {:else}
              <ul class="mt-3">
                {#each scopes as scope (scope)}
                  <li class="serial border-b border-rule py-2 text-[13.5px] text-ink last:border-b-0">
                    {scope}
                  </li>
                {/each}
              </ul>
            {/if}
          </div>

          {#if pending}
            <p class="mt-8 max-w-[62ch] text-[13px] leading-[1.6] text-muted">
              Approve only if you started this yourself on that device. Approving signs it in as you;
              refusing costs nothing and can be repeated.
            </p>

            <div class="mt-6 flex flex-wrap gap-3">
              <button
                type="button"
                class="act act-primary"
                disabled={busy}
                onclick={() => void decide("approve")}
              >
                {working === "approve" ? "Approving…" : "Approve this device"}
              </button>
              <button
                type="button"
                class="act act-seal"
                disabled={busy}
                onclick={() => void decide("deny")}
              >
                {working === "deny" ? "Refusing…" : "Refuse"}
              </button>
            </div>
          {:else}
            <p class="mt-8 max-w-[62ch] text-[13px] leading-[1.6] text-muted">
              This code has already been answered — it now stands as
              <span class="serial text-ink">{info.status}</span>. Start the sign-in again on the
              device to get a fresh code.
            </p>
          {/if}
        </div>
      </Section>
    {/if}
  {/if}
</Instrument>
