<script lang="ts">
  import Section from "../ui/Section.svelte";
  import Entry from "../ui/Entry.svelte";
  import BreakSeal from "../ui/BreakSeal.svelte";
  import Seal from "../ui/Seal.svelte";
  import { editor } from "../../lib/state/editor.svelte";
  import { session } from "../../lib/state/session.svelte";
  import { fieldText, formatStamp } from "../../lib/records";
  import type { AnyRecord } from "../../lib/api";

  /**
   * Temporary grants are written through their own endpoint, not through the
   * record document — so this panel commits on its own and only after the
   * principal exists.
   */
  const tempRoles = $derived(editor.temporaryItems("tmp_role_ids"));
  const tempPermissions = $derived(editor.temporaryItems("tmp_permission_ids"));
  const accessPath = $derived(`${session.apiBase}/${editor.spec.listPath}/${editor.loadedID}/access`);

  function startsAt(item: AnyRecord) {
    return formatStamp(fieldText(item.starts_at)) || "Now";
  }

  function expiresAt(item: AnyRecord) {
    return formatStamp(fieldText(item.expires_at)) || "No expiry";
  }
</script>

<Section
  title="Temporary access"
  note="Grants that withdraw themselves. Name the roles or permissions, give an expiry, and they stop applying without anyone having to remember them."
>
  {#snippet aside()}
    {#if editor.loadedID}
      <span class="stamp-raw serial">{accessPath}</span>
    {:else}
      <span class="stamp text-caution">Not yet issued</span>
    {/if}
  {/snippet}

  {#if !editor.loadedID}
    <p class="border border-dashed border-rule px-6 py-10 text-center text-[13px] leading-[1.6] text-muted">
      Temporary access is written to
      <span class="serial">{editor.spec.listPath}/&#123;id&#125;/access</span>, so the record has to
      exist first. Commit it, then reopen it from the register to grant.
    </p>
  {:else}
    <div class="grid gap-6 sm:grid-cols-2">
      <Entry
        label="Temp role IDs"
        bind:value={editor.temp.roleIDs}
        placeholder="admin, operator"
        mono
        hint="Comma or newline separated."
      />
      <Entry
        label="Temp permission IDs"
        bind:value={editor.temp.permissionIDs}
        placeholder="read-api, write-api"
        mono
        hint="Comma or newline separated."
      />
      <Entry
        label="Starts at"
        bind:value={editor.temp.startsAt}
        placeholder="optional RFC3339"
        mono
        hint="Empty starts the grant immediately."
      />
      <Entry
        label="Expires in"
        bind:value={editor.temp.expiresIn}
        placeholder="1h, 24h, 7d"
        mono
        hint="A duration from now. Takes precedence over an exact time."
      />
      <Entry
        label="Expires at"
        bind:value={editor.temp.expiresAt}
        placeholder="optional RFC3339"
        mono
        hint="Used only when no duration is given."
      />
    </div>

    <div class="mt-7">
      <button
        type="button"
        class="act act-primary"
        disabled={!editor.canGrantTemp}
        onclick={() => void editor.patchTemporaryAccess(false)}
      >
        {session.busy ? "Granting…" : "Grant temporary access"}
      </button>
      <p class="mt-2 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
        Granting again with the same IDs replaces their window rather than adding a second grant.
      </p>
    </div>

    <div class="mt-8">
      <BreakSeal
        consequence={`Withdrawing removes the named roles and permissions from ${editor.loadedID} straight away. Anything authorising through them stops working on the next request, the grant window is not kept, and there is no undo.`}
        action="Withdraw the named grants"
        disabled={!editor.canRemoveTemp}
        onconfirm={() => void editor.patchTemporaryAccess(true)}
      />
    </div>

    <div class="mt-10 grid gap-x-10 gap-y-8 sm:grid-cols-2">
      <div class="min-w-0">
        <h3 class="stamp stamp-ink border-b border-rule pb-1.5">Standing temporary roles</h3>
        {#if tempRoles.length === 0}
          <p class="mt-3 text-[13px] text-muted">None — this principal holds no temporary roles.</p>
        {:else}
          <ul class="mt-1">
            {#each tempRoles as item, index (`${fieldText(item.id)}-${index}`)}
              <li class="border-b border-rule py-2.5 last:border-b-0">
                <p class="serial truncate text-[13px] text-ink">{fieldText(item.id) || "—"}</p>
                <p class="serial mt-0.5 text-[12px] text-muted">
                  {startsAt(item)} until {expiresAt(item)}
                </p>
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <div class="min-w-0">
        <h3 class="stamp stamp-ink border-b border-rule pb-1.5">Standing temporary permissions</h3>
        {#if tempPermissions.length === 0}
          <p class="mt-3 text-[13px] text-muted">
            None — this principal holds no temporary permissions.
          </p>
        {:else}
          <ul class="mt-1">
            {#each tempPermissions as item, index (`${fieldText(item.id)}-${index}`)}
              <li class="border-b border-rule py-2.5 last:border-b-0">
                <p class="serial truncate text-[13px] text-ink">{fieldText(item.id) || "—"}</p>
                <p class="serial mt-0.5 text-[12px] text-muted">
                  {startsAt(item)} until {expiresAt(item)}
                </p>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    </div>

    {#if tempRoles.length > 0 || tempPermissions.length > 0}
      <p class="mt-6 flex items-center gap-2.5 text-[12.5px] text-muted">
        <Seal state="held" />
        These grants are held in the record and disappear on their own at the time shown.
      </p>
    {/if}
  {/if}
</Section>
