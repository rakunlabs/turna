<script lang="ts">
  import EditorSimpleForm from "./editor/EditorSimpleForm.svelte";
  import { kindSpecs } from "../lib/api";
  import type { AnyRecord, KindSpec, ResourceKind } from "../lib/api";

  export let editorKind: ResourceKind = "settings";
  export let editorSpec: KindSpec = kindSpecs.settings;
  export let apiBase = "/auth/v1";
  export let editorID = "";
  export let editorLoadedID = "";
  export let editorEnabled = true;
  export let editorJSON = "";
  export let advancedMode = false;
  export let editorJSONError = "";
  export let editorRequirementError = "";
  export let canCommit = false;
  export let simpleFormAvailable = true;
  export let busy = false;

  export let tempAccessRoleIDs = "";
  export let tempAccessPermissionIDs = "";
  export let tempAccessStartsAt = "";
  export let tempAccessExpiresIn = "1h";
  export let tempAccessExpiresAt = "";
  export let canGrantTemporaryAccess = false;
  export let canRemoveTemporaryAccess = false;

  export let closeEditor: () => void = () => {};
  export let setAdvancedMode: (enabled: boolean) => void = () => {};
  export let formatEditorJSON: () => void = () => {};
  export let loadEditorTemplate: () => void = () => {};
  export let saveResource: () => void | Promise<void> = () => {};
  export let applyNamespaceExample: (id?: string) => void = () => {};
  export let applyPermissionPreset: (name: string) => void = () => {};
  export let getStringField: (key: string) => string = () => "";
  export let setStringField: (key: string, value: string) => void = () => {};
  export let getBoolField: (key: string, fallback?: boolean) => boolean = () => false;
  export let setBoolField: (key: string, value: boolean) => void = () => {};
  export let setLocalUser: (value: boolean) => void = () => {};
  export let getListField: (key: string) => string = () => "";
  export let setListField: (key: string, value: string) => void = () => {};
  export let getNestedString: (parent: string, key: string) => string = () => "";
  export let setNestedString: (parent: string, key: string, value: string) => void = () => {};
  export let getFirstArrayString: (parent: string, key: string) => string = () => "";
  export let setFirstArrayString: (parent: string, key: string, value: string) => void = () => {};
  export let getFirstArrayList: (parent: string, key: string) => string = () => "";
  export let setFirstArrayList: (parent: string, key: string, value: string) => void = () => {};
  export let getJSONField: (key: string) => string = () => "{}";
  export let setJSONField: (key: string, value: string) => void = () => {};
  export let getPathString: (path: string[]) => string = () => "";
  export let setPathString: (path: string[], value: string) => void = () => {};
  export let setPathBool: (path: string[], value: boolean) => void = () => {};
  export let getPathBool: (path: string[], fallback?: boolean) => boolean = () => false;
  export let getPathNumber: (path: string[], fallback?: number) => number = () => 0;
  export let setPathNumber: (path: string[], value: string) => void = () => {};
  export let getPathList: (path: string[]) => string = () => "";
  export let setPathList: (path: string[], value: string) => void = () => {};
  export let permissionResources: () => AnyRecord[] = () => [];
  export let addPermissionResource: () => void = () => {};
  export let removePermissionResource: (index: number) => void = () => {};
  export let getResourceList: (index: number, key: string) => string = () => "";
  export let setResourceList: (index: number, key: string, value: string) => void = () => {};
  export let temporaryAccessItems: (key: "tmp_role_ids" | "tmp_permission_ids", json: string) => AnyRecord[] = () => [];
  export let patchTemporaryAccess: (remove?: boolean) => void | Promise<void> = () => {};

  function checkboxClass(checked: boolean) {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    return `${base} ${checked ? "border-line" : "border-alert"} checked:bg-fg`;
  }
</script>

<div class="bg-panel">
  <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
    <div class="flex items-center gap-3">
      <button class="btn-t border-0 bg-crt" disabled={busy} on:click={closeEditor}>[ &lt; BACK ]</button>
      <span class="t-label text-fg">
        {#if editorLoadedID}
          EDIT {editorSpec.title} / <span class="text-dim">{editorLoadedID}</span>
        {:else}
          NEW {editorSpec.title} / <span class="text-alert">DRAFT</span>
        {/if}
      </span>
    </div>
    <div class="flex flex-wrap gap-px">
      <button class={`btn-t border-0 ${advancedMode ? "bg-alert text-white hover:bg-alert hover:text-white" : "bg-crt"}`} disabled={busy} on:click={() => setAdvancedMode(!advancedMode)}>
        {advancedMode ? "SIMPLE FORM" : "ADVANCED JSON"}
      </button>
      {#if advancedMode}
        <button class="btn-t border-0 bg-crt" disabled={busy} on:click={formatEditorJSON}>FORMAT JSON</button>
      {/if}
      {#if !editorLoadedID}
        <button class="btn-t border-0 bg-crt" disabled={busy} on:click={loadEditorTemplate}>LOAD TEMPLATE</button>
      {/if}
      <button class="btn-t-solid" disabled={!canCommit} on:click={saveResource}>[ COMMIT ]</button>
    </div>
  </div>

  {#if editorRequirementError}
    <p class="border-b border-line bg-panel px-4 py-2 text-[11px] font-bold uppercase tracking-[0.12em] text-alert">{editorRequirementError}</p>
  {/if}

  <div class="grid gap-px bg-line p-px">
    <div class="grid gap-px bg-line lg:grid-cols-[minmax(220px,1fr),minmax(220px,1fr),auto]">
      <div class="grid gap-1 bg-panel p-3 lg:col-span-1">
        <span class="t-label">FORM</span>
        <p class="text-sm font-bold uppercase tracking-[0.08em] text-fg">{editorSpec.title}</p>
      </div>

      {#if editorSpec.body !== "raw" && editorKind === "settings"}
        <div class="grid gap-1 bg-panel p-3 lg:col-span-1">
          <span class="t-label">NAMESPACE</span>
          <p class="text-sm font-bold uppercase tracking-[0.08em] text-fg">{editorID || "SELECT RESERVED"}</p>
        </div>
      {:else if editorSpec.body !== "raw"}
        <label class="grid gap-1 bg-panel p-3 lg:col-span-1">
          <span class="t-label">{editorSpec.idField}</span>
          <input bind:value={editorID} class="field-t" placeholder="default" on:input={() => applyNamespaceExample(editorID)} />
        </label>
      {/if}

      {#if editorSpec.body === "config"}
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input bind:checked={editorEnabled} type="checkbox" class={checkboxClass(editorEnabled)} />
          <span class={editorEnabled ? "text-fg" : "text-alert"}>{editorEnabled ? "ENABLED" : "DISABLED"}</span>
        </label>
      {/if}
    </div>

    {#if editorKind === "permissions" && !editorLoadedID}
      <div class="grid gap-2 bg-panel p-3">
        <span class="t-label">QUICK TEMPLATES</span>
        <div class="flex flex-wrap gap-px">
          <button
            class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-dim hover:border-fg hover:text-fg"
            disabled={busy}
            on:click={() => applyPermissionPreset("auth_admin")}
          >
            auth_admin
          </button>
          <button
            class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-dim hover:border-fg hover:text-fg"
            disabled={busy}
            on:click={() => applyPermissionPreset("auth_user")}
          >
            auth_user
          </button>
        </div>
        <span class="text-[10px] leading-4 text-dim">
          Pre-fill resources for an admin (full /v1 access) or a normal self-service user. Paths use the current auth prefix — review before committing, then set this permission's name on the admin user (and Admin → permission = auth_admin).
        </span>
      </div>
    {/if}

    {#if editorSpec.namespaceExamples && !editorLoadedID}
      <div class="grid gap-2 bg-panel p-3">
        <span class="t-label">RESERVED NAMESPACES</span>
        <div class="flex flex-wrap gap-px">
          {#each Object.keys(editorSpec.namespaceExamples ?? {}) as ns}
            <button
              class={`border px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] ${
                editorID.trim() === ns
                  ? "border-alert bg-alert text-white"
                  : "border-line text-dim hover:border-fg hover:text-fg"
              }`}
              on:click={() => {
                editorID = ns;
                applyNamespaceExample(ns);
              }}
            >
              {ns}
            </button>
          {/each}
        </div>
      </div>
    {/if}

    {#if advancedMode}
      <div class="grid min-w-0 content-start gap-px bg-line">
        <div class="flex flex-wrap items-center justify-between gap-2 bg-panel px-3 py-2">
          <span class="t-label text-fg">PAYLOAD JSON</span>
          <span class={`text-[10px] font-bold uppercase tracking-[0.15em] ${editorJSONError ? "text-alert" : "text-phosphor"}`}>
            {editorJSONError ? "INVALID" : "VALID"}
          </span>
        </div>
        {#if editorJSONError}
          <p class="bg-panel px-3 py-2 text-[11px] leading-4 text-alert">{editorJSONError}</p>
        {/if}
        <textarea
          bind:value={editorJSON}
          class="field-t min-h-[420px] border-0 bg-panel text-[13px] leading-6"
          aria-invalid={Boolean(editorJSONError)}
          spellcheck="false"
        ></textarea>
      </div>
    {:else if !simpleFormAvailable}
      <div class="grid gap-3 bg-panel p-4">
        <p class="text-sm font-bold uppercase tracking-[0.12em] text-fg">No simple form for this namespace yet.</p>
        <p class="max-w-2xl text-xs leading-5 text-dim">Reserved settings namespaces use field-based forms. Existing unsupported namespaces can still be reviewed in Advanced JSON.</p>
        <button class="btn-t-solid w-fit" on:click={() => setAdvancedMode(true)}>OPEN ADVANCED JSON</button>
      </div>
    {:else}
      <EditorSimpleForm
        {editorKind}
        {editorSpec}
        {apiBase}
        {editorID}
        {editorLoadedID}
        {editorJSON}
        bind:tempAccessRoleIDs
        bind:tempAccessPermissionIDs
        bind:tempAccessStartsAt
        bind:tempAccessExpiresIn
        bind:tempAccessExpiresAt
        {canGrantTemporaryAccess}
        {canRemoveTemporaryAccess}
        {getStringField}
        {setStringField}
        {getBoolField}
        {setBoolField}
        {setLocalUser}
        {getListField}
        {setListField}
        {getNestedString}
        {setNestedString}
        {getFirstArrayString}
        {setFirstArrayString}
        {getFirstArrayList}
        {setFirstArrayList}
        {getJSONField}
        {setJSONField}
        {getPathString}
        {setPathString}
        {setPathBool}
        {getPathBool}
        {getPathNumber}
        {setPathNumber}
        {getPathList}
        {setPathList}
        {permissionResources}
        {addPermissionResource}
        {removePermissionResource}
        {getResourceList}
        {setResourceList}
        {temporaryAccessItems}
        {patchTemporaryAccess}
      />
    {/if}

    {#if editorSpec.body === "raw" && !editorLoadedID}
      <p class="bg-panel p-3 text-[10px] leading-4 tracking-[0.05em] text-dim">
        NEW RECORDS GET A GENERATED ID ON COMMIT WHEN THE API OWNS THE ID FIELD.
      </p>
    {/if}
  </div>
</div>
