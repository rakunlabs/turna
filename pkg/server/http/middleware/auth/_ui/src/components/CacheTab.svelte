<script lang="ts">
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

  const pollInterval = $derived(getSettingString("cache", ["poll_interval"]));
  const codeStore = $derived(getSettingString("cache", ["code_store", "active"]) || "database");
  const redisTLS = $derived(getSettingBool("cache", ["code_store", "redis", "tls", "enabled"]));
  const addresses = $derived(getSettingList("cache", ["code_store", "redis", "address"]));

  const shared = $derived(codeStore !== "memory");
</script>

<Instrument
  title="Cache"
  note="Two pieces of shared state: how instances recover changes missed by PostgreSQL notifications, and where short-lived OAuth codes live while a flow is in progress."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("cache")}
    >
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">cache</span></span>
    <Seal
      state={shared ? "endorsed" : "held"}
      label={shared ? "Shared across instances" : "Single instance only"}
    />
  {/snippet}

  <Section title="Change propagation" first>
    <div class="max-w-[52ch]">
      <label class="stamp block" for="cache-poll-interval">Fallback poll interval</label>
      <input
        id="cache-poll-interval"
        class="entry serial mt-1.5"
        autocomplete="off"
        placeholder="5s"
        aria-describedby="cache-poll-interval-hint"
        value={pollInterval}
        oninput={(e) => setSettingString("cache", ["poll_interval"], e.currentTarget.value)}
      />
      <p id="cache-poll-interval-hint" class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
        Instances normally reload immediately through PostgreSQL LISTEN/NOTIFY. This Go duration
        controls the fallback version check that recovers notifications missed during a disconnect.
        The instance you committed on still applies the change synchronously.
      </p>
    </div>
  </Section>

  <Section
    title="OAuth code store"
    note="Authorization codes, provider callback state and passkey challenges are held here briefly while a flow is in progress. They are not a cache of records — losing them fails whichever logins were mid-flight."
  >
    <div class="max-w-[52ch]">
      <label class="stamp block" for="cache-code-store">Store</label>
      <select
        id="cache-code-store"
        class="entry mt-1.5"
        aria-describedby="cache-code-store-hint"
        value={codeStore}
        onchange={(e) => setSettingString("cache", ["code_store", "active"], e.currentTarget.value)}
      >
        <option value="database">database</option>
        <option value="memory">memory</option>
        <option value="redis">redis</option>
      </select>
      <p id="cache-code-store-hint" class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
        Database is the shared default and uses PostgreSQL. Redis is an optional shared backend.
        Memory is safe only when exactly one instance serves every step of every flow.
      </p>
    </div>
  </Section>

  {#if codeStore === "redis"}
    <Section title="Redis connection">
      <div class="grid gap-6">
        <div class="max-w-[62ch]">
          <label class="stamp block" for="cache-redis-address">Addresses</label>
          <input
            id="cache-redis-address"
            class="entry serial mt-1.5"
            autocomplete="off"
            spellcheck="false"
            placeholder="127.0.0.1:6379"
            aria-describedby="cache-redis-address-hint"
            value={addresses}
            oninput={(e) =>
              setSettingList("cache", ["code_store", "redis", "address"], e.currentTarget.value)}
          />
          <p id="cache-redis-address-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
            Comma separated. More than one address is treated as a cluster.
          </p>
        </div>

        <div class="grid gap-6 sm:grid-cols-2">
          <div>
            <label class="stamp block" for="cache-redis-username">Username</label>
            <input
              id="cache-redis-username"
              class="entry mt-1.5"
              autocomplete="off"
              placeholder="optional"
              value={getSettingString("cache", ["code_store", "redis", "username"])}
              oninput={(e) =>
                setSettingString("cache", ["code_store", "redis", "username"], e.currentTarget.value)}
            />
          </div>

          <div>
            <label class="stamp block" for="cache-redis-password">Password</label>
            <input
              id="cache-redis-password"
              class="entry mt-1.5"
              autocomplete="off"
              placeholder="optional"
              value={getSettingString("cache", ["code_store", "redis", "password"])}
              oninput={(e) =>
                setSettingString("cache", ["code_store", "redis", "password"], e.currentTarget.value)}
            />
          </div>

          <div>
            <label class="stamp block" for="cache-redis-client-name">Client name</label>
            <input
              id="cache-redis-client-name"
              class="entry mt-1.5"
              autocomplete="off"
              placeholder="turna-auth"
              aria-describedby="cache-redis-client-name-hint"
              value={getSettingString("cache", ["code_store", "redis", "client_name"])}
              oninput={(e) =>
                setSettingString(
                  "cache",
                  ["code_store", "redis", "client_name"],
                  e.currentTarget.value,
                )}
            />
            <p id="cache-redis-client-name-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              How this instance names itself in Redis client lists.
            </p>
          </div>
        </div>
      </div>
    </Section>

    <Section title="Redis TLS">
      <Switch
        label="Connect to Redis over TLS"
        hint="The codes in flight are single-use secrets. Turn this on whenever Redis is not on the same host."
        bind:checked={
          () => getSettingBool("cache", ["code_store", "redis", "tls", "enabled"]),
          (value: boolean) =>
            setSettingBool("cache", ["code_store", "redis", "tls", "enabled"], value)
        }
      />

      {#if redisTLS}
        <div class="mt-6 grid gap-6 sm:grid-cols-3">
          <div>
            <label class="stamp block" for="cache-redis-ca">CA file</label>
            <input
              id="cache-redis-ca"
              class="entry serial mt-1.5"
              autocomplete="off"
              spellcheck="false"
              placeholder="optional"
              value={getSettingString("cache", ["code_store", "redis", "tls", "ca_file"])}
              oninput={(e) =>
                setSettingString(
                  "cache",
                  ["code_store", "redis", "tls", "ca_file"],
                  e.currentTarget.value,
                )}
            />
          </div>

          <div>
            <label class="stamp block" for="cache-redis-cert">Certificate file</label>
            <input
              id="cache-redis-cert"
              class="entry serial mt-1.5"
              autocomplete="off"
              spellcheck="false"
              placeholder="optional"
              value={getSettingString("cache", ["code_store", "redis", "tls", "cert_file"])}
              oninput={(e) =>
                setSettingString(
                  "cache",
                  ["code_store", "redis", "tls", "cert_file"],
                  e.currentTarget.value,
                )}
            />
          </div>

          <div>
            <label class="stamp block" for="cache-redis-key">Key file</label>
            <input
              id="cache-redis-key"
              class="entry serial mt-1.5"
              autocomplete="off"
              spellcheck="false"
              placeholder="optional"
              value={getSettingString("cache", ["code_store", "redis", "tls", "key_file"])}
              oninput={(e) =>
                setSettingString(
                  "cache",
                  ["code_store", "redis", "tls", "key_file"],
                  e.currentTarget.value,
                )}
            />
          </div>
        </div>

        <p class="mt-4 max-w-[70ch] text-[12.5px] leading-[1.55] text-muted">
          All three are paths on the auth container's filesystem. Leave them empty to verify the
          server against the system trust store without presenting a client certificate.
        </p>
      {/if}
    </Section>
  {/if}
</Instrument>
