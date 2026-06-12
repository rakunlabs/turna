<script lang="ts">
  import { onMount } from "svelte";
  import type { AnyRecord } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let initialUserCode = "";

  type DeviceInfo = { client_id: string; scope: string; status: string };

  let userCode = "";
  let info: DeviceInfo | null = null;
  let busy = false;
  let error = "";
  let done = "";

  function normalize(value: string) {
    const flat = value.toUpperCase().replace(/[\s-]+/g, "");
    return flat.length === 8 ? `${flat.slice(0, 4)}-${flat.slice(4)}` : value.toUpperCase();
  }

  async function api<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${apiBase}/${path}`, {
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      ...init,
    });

    let body: AnyRecord = {};
    try {
      body = await res.json();
    } catch {
      // ignore empty bodies
    }

    if (!res.ok) {
      throw new Error(String(body.message ?? body.error ?? res.statusText));
    }

    return body.payload as T;
  }

  async function lookup() {
    busy = true;
    error = "";
    done = "";
    info = null;
    try {
      info = await api<DeviceInfo>(`device/${encodeURIComponent(normalize(userCode))}`);
    } catch (err) {
      error = err instanceof Error ? err.message : "DEVICE CODE NOT FOUND";
    } finally {
      busy = false;
    }
  }

  async function decide(action: "approve" | "deny") {
    busy = true;
    error = "";
    try {
      await api("device", {
        method: "POST",
        body: JSON.stringify({ user_code: normalize(userCode), action }),
      });

      done = action === "approve" ? "DEVICE APPROVED — RETURN TO YOUR DEVICE" : "DEVICE DENIED";
      info = null;
      userCode = "";
    } catch (err) {
      error = err instanceof Error ? err.message : "OPERATION FAILED";
    } finally {
      busy = false;
    }
  }

  onMount(() => {
    if (initialUserCode) {
      userCode = normalize(initialUserCode);
      void lookup();
    }
  });
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <span class="t-label text-fg">[ DEVICE LOGIN ]</span>
    <p class="max-w-2xl text-[11px] leading-4 text-dim">
      A device (CLI, TV, ...) showed you a code. Enter it here to approve or deny the login.
      Approval signs the device in <span class="text-fg">as your account</span>.
    </p>

    {#if error}
      <p class="text-[11px] font-bold uppercase tracking-[0.12em] text-alert">{error}</p>
    {/if}
    {#if done}
      <p class="text-[11px] font-bold uppercase tracking-[0.12em] text-fg">{done}</p>
    {/if}

    <div class="grid gap-px bg-line md:grid-cols-[minmax(0,1fr),auto]">
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">USER CODE</span>
        <input
          bind:value={userCode}
          class="field-t text-center text-lg tracking-[0.3em]"
          placeholder="XXXX-XXXX"
          maxlength="9"
          on:keydown={(event) => event.key === "Enter" && lookup()}
        />
      </label>
      <div class="flex items-end bg-panel p-3">
        <button class="btn-t-solid w-full" disabled={busy || userCode.replace(/[\s-]+/g, "").length !== 8} on:click={lookup}>
          [ LOOK UP ]
        </button>
      </div>
    </div>

    {#if info}
      <div class="grid gap-2 border border-line p-3">
        <span class="t-label">PENDING REQUEST</span>
        <div class="grid gap-1 text-[11px] leading-5">
          <p><span class="text-dim">CLIENT</span> <span class="font-bold text-fg">{info.client_id}</span></p>
          {#if info.scope}<p><span class="text-dim">SCOPE</span> {info.scope}</p>{/if}
          <p><span class="text-dim">STATUS</span> {info.status.toUpperCase()}</p>
        </div>

        {#if info.status === "pending"}
          <div class="flex flex-wrap gap-2">
            <button class="btn-t-solid" disabled={busy} on:click={() => decide("approve")}>[ APPROVE ]</button>
            <button
              class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
              disabled={busy}
              on:click={() => decide("deny")}
            >
              DENY
            </button>
          </div>
        {:else}
          <p class="text-[11px] text-dim">This code has already been handled.</p>
        {/if}
      </div>
    {/if}
  </div>
</div>
