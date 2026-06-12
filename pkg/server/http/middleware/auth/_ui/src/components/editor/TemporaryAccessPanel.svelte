<script lang="ts">
  import type { AnyRecord, KindSpec } from "../../lib/api";

  export let editorSpec: KindSpec;
  export let editorLoadedID = "";
  export let editorJSON = "";
  export let tempAccessRoleIDs = "";
  export let tempAccessPermissionIDs = "";
  export let tempAccessStartsAt = "";
  export let tempAccessExpiresIn = "1h";
  export let tempAccessExpiresAt = "";
  export let canGrantTemporaryAccess = false;
  export let canRemoveTemporaryAccess = false;
  export let temporaryAccessItems: (key: "tmp_role_ids" | "tmp_permission_ids", json: string) => AnyRecord[] = () => [];
  export let patchTemporaryAccess: (remove?: boolean) => void | Promise<void> = () => {};

  function fieldText(value: unknown) {
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value.trim() : String(value).trim();
  }
</script>

<div class="grid gap-3 bg-panel p-3 md:col-span-2 xl:col-span-3">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div>
      <span class="t-label text-fg">[ TEMPORARY ACCESS ]</span>
      <p class="mt-1 text-[11px] leading-4 text-dim">Grant role or permission IDs until a duration or exact expiration. Remove sends the same IDs without expiration.</p>
    </div>
    <span class="t-label">{editorLoadedID ? "READY" : "CREATE FIRST"}</span>
  </div>

  {#if editorLoadedID}
    <div class="grid gap-px bg-line md:grid-cols-2 xl:grid-cols-3">
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">TEMP ROLE IDS</span>
        <input bind:value={tempAccessRoleIDs} class="field-t" placeholder="admin, operator" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">TEMP PERMISSION IDS</span>
        <input bind:value={tempAccessPermissionIDs} class="field-t" placeholder="read-api, write-api" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">STARTS AT</span>
        <input bind:value={tempAccessStartsAt} class="field-t" placeholder="optional RFC3339" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">EXPIRES IN</span>
        <input bind:value={tempAccessExpiresIn} class="field-t" placeholder="1h, 24h, 7d" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">EXPIRES AT</span>
        <input bind:value={tempAccessExpiresAt} class="field-t" placeholder="optional RFC3339" />
      </label>
      <div class="flex flex-wrap items-end gap-px bg-panel p-3">
        <button class="btn-t-solid flex-1" disabled={!canGrantTemporaryAccess} on:click={() => patchTemporaryAccess(false)}>[ GRANT / UPDATE ]</button>
        <button class="btn-t flex-1 border-0 bg-crt text-alert" disabled={!canRemoveTemporaryAccess} on:click={() => patchTemporaryAccess(true)}>[ REMOVE ]</button>
      </div>
    </div>

    <div class="grid gap-px bg-line md:grid-cols-2">
      <div class="grid gap-2 bg-panel p-3">
        <span class="t-label text-fg">CURRENT TEMP ROLES</span>
        {#if temporaryAccessItems("tmp_role_ids", editorJSON).length === 0}
          <p class="text-[11px] leading-4 text-dim">No temporary roles.</p>
        {:else}
          <div class="grid gap-px bg-line">
            {#each temporaryAccessItems("tmp_role_ids", editorJSON) as item}
              <div class="grid gap-1 bg-crt p-2 text-[11px] leading-4">
                <span class="font-bold text-fg">{fieldText(item.id)}</span>
                <span class="text-dim">START {fieldText(item.starts_at) || "NOW"} / EXPIRE {fieldText(item.expires_at) || "N/A"}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <div class="grid gap-2 bg-panel p-3">
        <span class="t-label text-fg">CURRENT TEMP PERMISSIONS</span>
        {#if temporaryAccessItems("tmp_permission_ids", editorJSON).length === 0}
          <p class="text-[11px] leading-4 text-dim">No temporary permissions.</p>
        {:else}
          <div class="grid gap-px bg-line">
            {#each temporaryAccessItems("tmp_permission_ids", editorJSON) as item}
              <div class="grid gap-1 bg-crt p-2 text-[11px] leading-4">
                <span class="font-bold text-fg">{fieldText(item.id)}</span>
                <span class="text-dim">START {fieldText(item.starts_at) || "NOW"} / EXPIRE {fieldText(item.expires_at) || "N/A"}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {:else}
    <p class="text-[11px] leading-4 text-dim">Temporary access uses <span class="text-fg">/{editorSpec.listPath}/{"{id}"}/access</span>, so create the record first.</p>
  {/if}
</div>
