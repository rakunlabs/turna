<script lang="ts">
  import CheckWidget from "./CheckWidget.svelte";
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import { session } from "../lib/state/session.svelte";
  import {
    getSettingBool,
    setSettingBool,
    getSettingList,
    setSettingList,
    saveSetting,
  } from "../lib/state/settings.svelte";
  import { splitValues } from "../lib/records";

  /**
   * Two things live here, and only one of them is the point. The check answers
   * "does this identity get through?"; the rules below are the small amount of
   * configuration that changes how that question is decided.
   */
  const defaultHosts = $derived(getSettingList("check", ["default_hosts"]));
  const hostList = $derived(splitValues(defaultHosts));
</script>

<Instrument
  title="Access check"
  note="Ask the live permission graph whether one identity gets through to one request. Read-only: nothing is written and no token is issued."
>
  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">check</span></span>
    <span class="serial stamp-raw">{session.apiBase}/check</span>
  {/snippet}

  <CheckWidget />

  <Section
    title="Matching rules"
    note="How every permission in this instance is matched — against the check above and against every request the middleware sees."
  >
    {#snippet aside()}
      <button
        type="button"
        class="act act-primary"
        disabled={session.busy}
        onclick={() => void saveSetting("check")}
      >
        {session.busy ? "Committing…" : "Commit"}
      </button>
    {/snippet}

    <div class="grid gap-8 lg:grid-cols-2">
      <div class="min-w-0">
        <label class="stamp block" for="check-default-hosts">Default hosts</label>
        <textarea
          id="check-default-hosts"
          class="exhibit mt-1.5 min-h-24"
          spellcheck="false"
          aria-describedby="check-default-hosts-hint"
          placeholder="api.example.com"
          value={defaultHosts}
          oninput={(event) => setSettingList("check", ["default_hosts"], event.currentTarget.value)}
        ></textarea>
        <p id="check-default-hosts-hint" class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
          Used only by permissions that name no host of their own. One per line, or comma separated.
          Glob patterns are accepted.
        </p>

        <h3 class="stamp stamp-ink mt-6 border-b border-rule pb-1.5">In effect</h3>
        {#if hostList.length === 0}
          <p class="border-b border-rule py-3 text-[12.5px] leading-[1.55] text-muted">
            No default host set. With host checking on, a permission that names no host of its own
            cannot match anything — add a host here or name hosts on the permission itself.
          </p>
        {:else}
          <ul>
            {#each hostList as host (host)}
              <li class="serial border-b border-rule py-2 text-[13px] text-ink last:border-b-0">
                {host}
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="min-w-0">
        <Switch
          label="Disable host checking"
          consequential
          hint="Permissions match on method and path alone, on every host. This widens what every existing permission accepts — leave it off unless this instance fronts a single host."
          bind:checked={
            () => getSettingBool("check", ["no_host_check"]),
            (value: boolean) => setSettingBool("check", ["no_host_check"], value)
          }
        />

        <p class="mt-6 max-w-[62ch] text-[12.5px] leading-[1.6] text-muted">
          Both values apply without a restart. They take effect on the next request the middleware
          handles, and on the next check run above.
        </p>
      </div>
    </div>
  </Section>
</Instrument>
