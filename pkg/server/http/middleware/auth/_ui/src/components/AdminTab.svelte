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
    saveSetting,
  } from "../lib/state/settings.svelte";

  const permission = $derived(getSettingString("admin", ["permission"]));
  const allowMissing = $derived(getSettingBool("admin", ["allow_missing_x_user"], true));

  /** What the running instance currently enforces, as opposed to the draft above. */
  const enforced = $derived(session.capabilities?.admin_permission ?? "");
  const configured = $derived(session.capabilities?.admin_permission_configured === true);
  const breakGlassActive = $derived(session.capabilities?.anonymous_admin === true);
  const identity = $derived(session.capabilities?.x_user ?? "");
</script>

<Instrument
  title="Admin access"
  note="Who may administer this instance. The permission below is matched against the permission IDs and names carried by the X-User header that session forwards."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("admin")}
    >
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">admin</span></span>
    <Seal
      state={breakGlassActive ? "broken" : configured ? "endorsed" : "held"}
      label={breakGlassActive ? "Break-glass open" : configured ? "Permission enforced" : "Bootstrap open"}
    />
  {/snippet}

  {#if breakGlassActive}
    <p class="hatch mb-10 max-w-[70ch] border border-seal/45 px-4 py-3.5 text-[13px] leading-[1.6] text-ink">
      <span class="stamp text-seal">Standing now</span>
      <span class="mt-1.5 block">
        This very request carried no identity and administered the instance anyway. Anything that can
        reach this route can do the same. Keep it that way only while you are recovering an instance
        that is not publicly reachable.
      </span>
    </p>
  {/if}

  <Section title="Required permission" first>
    <div class="max-w-[62ch]">
      <label class="stamp block" for="admin-permission">Admin permission</label>
      <input
        id="admin-permission"
        class="entry serial mt-1.5"
        autocomplete="off"
        spellcheck="false"
        placeholder="turna.auth.admin"
        aria-describedby="admin-permission-hint"
        value={permission}
        oninput={(e) => setSettingString("admin", ["permission"], e.currentTarget.value)}
      />
      <p id="admin-permission-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
        Matched against the permission ID or the permission name on X-User. Leave it empty and every
        authenticated request administers this instance — that is the bootstrap state, not a setting
        to stay in. Create the permission first, grant it to a role, then name it here.
      </p>
    </div>

    <div class="mt-8 flex flex-wrap items-end gap-x-10 gap-y-5">
      <div class="min-w-0">
        <Serial value={enforced || "not set"} size="md" tone={configured ? "ink" : "seal"} />
        <p class="stamp mt-2">Enforced right now</p>
      </div>

      <p class="max-w-[52ch] flex-1 basis-72 text-[13px] leading-[1.6] text-muted">
        {configured
          ? "Committing a change here takes effect on the next request — including yours. Make sure you hold the new permission before you commit it."
          : "Nothing is required yet, so any authenticated caller can write here. Naming a permission is the step that closes this instance."}
      </p>
    </div>
  </Section>

  <Section
    title="Break-glass"
    note="A request that arrives with no X-User has no identity at all: no user, no role, no permission to check."
  >
    {#snippet aside()}
      <span class="stamp {allowMissing ? 'text-seal' : 'text-endorsed'}">
        {allowMissing ? "Granted" : "Refused"}
      </span>
    {/snippet}

    <Switch
      label="Administer without an identity when X-User is missing"
      consequential
      hint="On, a request with no X-User is treated as a full administrator of this instance — no user, no role and no permission are checked. It exists so you can recover an instance whose admin permission locked you out."
      bind:checked={
        () => getSettingBool("admin", ["allow_missing_x_user"], true),
        (value: boolean) => setSettingBool("admin", ["allow_missing_x_user"], value)
      }
    />

    {#if allowMissing}
      <p class="hatch mt-6 max-w-[70ch] border border-seal/40 px-4 py-3 text-[13px] leading-[1.55] text-ink">
        <span class="stamp text-seal">Do not leave this reachable</span>
        <span class="mt-1.5 block">
          While this is on, this route must not be publicly reachable. Anyone who reaches it directly
          — bypassing session, or hitting the container's port, or inside the cluster network — can
          create users, grant permissions and read every encrypted config, with no credential at all.
          Bind it to localhost or keep it behind the session chain, and turn this off once a real
          admin permission works.
        </span>
      </p>
    {/if}
  </Section>

  <Section title="This request" note="What the instance made of the credentials you are reading with.">
    <ul class="max-w-[70ch]">
      <li class="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-rule py-3">
        <span class="flex shrink-0 items-center gap-2.5">
          <Seal state={identity ? "endorsed" : "void"} />
          <span class="text-[13.5px] text-ink">Identity forwarded</span>
        </span>
        <span class="serial min-w-0 flex-1 basis-64 truncate text-[12.5px] text-muted">
          {identity || "no X-User on this request"}
        </span>
      </li>
      <li class="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-rule py-3">
        <span class="flex shrink-0 items-center gap-2.5">
          <Seal state={configured ? "endorsed" : "held"} />
          <span class="text-[13.5px] text-ink">Permission required</span>
        </span>
        <span class="min-w-0 flex-1 basis-64 text-[12.5px] leading-[1.5] text-muted">
          {configured ? "Checked on every admin call" : "Bootstrap — every authenticated caller is an admin"}
        </span>
      </li>
      <li class="flex flex-wrap items-baseline gap-x-4 gap-y-1 py-3">
        <span class="flex shrink-0 items-center gap-2.5">
          <Seal state={breakGlassActive ? "broken" : "endorsed"} />
          <span class="text-[13.5px] text-ink">Break-glass in use</span>
        </span>
        <span class="min-w-0 flex-1 basis-64 text-[12.5px] leading-[1.5] text-muted">
          {breakGlassActive
            ? "This session is administering without any identity"
            : "This session was admitted with an identity"}
        </span>
      </li>
    </ul>
  </Section>
</Instrument>
