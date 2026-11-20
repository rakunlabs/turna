<script lang="ts">
  import { untrack } from "svelte";
  import { KeyRound, UserX, Ghost, LoaderCircle, Server, ChevronDown, CircleAlert } from "@lucide/svelte";
  import { api, ApiError, type BindMode, type Info, type Session } from "../lib/api";
  import ThemeToggle from "./ThemeToggle.svelte";

  let { info, onlogin }: { info: Info; onlogin: (s: Session) => void } = $props();

  const CUSTOM = "\u0000custom";
  const saved = (() => {
    try {
      return JSON.parse(localStorage.getItem("turna-lv-login") ?? "{}");
    } catch {
      return {};
    }
  })();

  const firstServer = () => {
    if (saved.server && info.servers.some((s) => s.name === saved.server)) return saved.server as string;
    if (saved.server === CUSTOM) return CUSTOM;
    return info.servers[0]?.name ?? CUSTOM;
  };

  let server = $state(firstServer());
  let addr = $state<string>(saved.addr ?? "ldap://localhost:389");
  let startTLS = $state<boolean>(saved.start_tls ?? false);
  let skipVerify = $state<boolean>(saved.insecure_skip_verify ?? false);
  // a CA certificate is public, so keeping it in local storage is fine
  let ca = $state<string>(saved.ca ?? "");
  const hasCA = $derived(ca.includes("BEGIN CERTIFICATE"));
  const tlsInUse = $derived(addr.trim().startsWith("ldaps://") || startTLS);

  async function loadCA(e: Event) {
    const file = (e.currentTarget as HTMLInputElement).files?.[0];
    if (file) ca = await file.text();
  }
  let mode = $state<BindMode>(untrack(() => (info.bind_modes.includes(saved.mode) ? saved.mode : info.bind_modes[0])));
  let username = $state<string>(saved.username ?? "");
  let password = $state("");
  let baseDN = $state<string>(saved.base_dn ?? "");
  let busy = $state(false);
  let error = $state<string | null>(null);
  let advanced = $state(false);

  const custom = $derived(server === CUSTOM);
  const selected = $derived(info.servers.find((s) => s.name === server));

  const modes: Record<BindMode, { label: string; short: string; hint: string; icon: typeof KeyRound }> = {
    simple: { label: "Credentials", short: "Credentials", hint: "Simple bind with your DN or username and password.", icon: KeyRound },
    anonymous: { label: "Anonymous", short: "Anonymous", hint: "Empty DN and empty password; sees what the server allows anyone to read.", icon: Ghost },
    unauthenticated: {
      label: "Unauthenticated",
      short: "Unauth.",
      hint: "A DN with an empty password (RFC 4513 5.1.2). Most servers refuse it unless enabled.",
      icon: UserX,
    },
  };

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    error = null;
    busy = true;
    try {
      const s = await api.login({
        server: custom ? undefined : server,
        addr: custom ? addr.trim() : undefined,
        start_tls: custom ? startTLS : undefined,
        insecure_skip_verify: custom ? skipVerify : undefined,
        ca: custom && ca.trim() ? ca : undefined,
        base_dn: baseDN.trim() || undefined,
        mode,
        username: mode === "anonymous" ? undefined : username,
        password: mode === "simple" ? password : undefined,
      });
      localStorage.setItem(
        "turna-lv-login",
        JSON.stringify({ server, addr, start_tls: startTLS, insecure_skip_verify: skipVerify, ca, mode, username, base_dn: baseDN }),
      );
      password = "";
      onlogin(s);
    } catch (err) {
      error = err instanceof ApiError ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<div class="flex min-h-full items-start justify-center p-4 pt-[max(1rem,12vh)]">
  <form
    onsubmit={submit}
    class="relative w-full max-w-[420px] rounded-xl border border-line bg-surface p-6 shadow-[var(--shadow)]"
    autocomplete="on"
  >
    <div class="mb-5 flex items-center gap-2.5 pr-8">
      <div class="flex size-8 items-center justify-center rounded-lg bg-accent text-accent-ink">
        <Server class="size-4" />
      </div>
      <div>
        <h1 class="text-[15px] leading-tight font-semibold">{info.title || "LDAP"}</h1>
        <p class="text-muted">Connect to a directory to browse and search it.</p>
      </div>
    </div>

    <label class="mb-3 block">
      <span class="mb-1 block text-[12px] font-medium text-muted">Server</span>
      {#if info.servers.length > 0}
        <div class="relative">
          <select class="input appearance-none pr-8" bind:value={server}>
            {#each info.servers as s (s.name)}
              <option value={s.name}>{s.name === s.addr ? s.addr : `${s.name} — ${s.addr}`}</option>
            {/each}
            <option value={CUSTOM}>Custom URL…</option>
          </select>
          <ChevronDown class="pointer-events-none absolute top-2 right-2 size-4 text-faint" />
        </div>
      {/if}
      {#if custom}
        <input
          class="input mt-2 font-mono"
          bind:value={addr}
          placeholder="ldap://host:389 or ldaps://host:636"
          spellcheck="false"
          required
        />
      {/if}
    </label>

    <fieldset class="mb-3">
      <legend class="mb-1 block text-[12px] font-medium text-muted">Bind</legend>
      <div class="grid gap-1 rounded-lg bg-sunk p-1" style="grid-template-columns: repeat({info.bind_modes.length}, minmax(0, 1fr))">
        {#each info.bind_modes as m (m)}
          {@const M = modes[m].icon}
          <label
            class="flex h-8 min-w-0 items-center justify-center gap-1.5 rounded-md px-1 text-[12.5px] font-medium transition-colors
              {mode === m ? 'bg-surface text-ink shadow-sm' : 'text-muted hover:text-ink'}"
          >
            <input type="radio" class="sr-only" name="mode" value={m} bind:group={mode} />
            <M class="hidden size-3.5 shrink-0 sm:block" />
            <span class="truncate sm:hidden" title={modes[m].label}>{modes[m].short}</span>
            <span class="hidden truncate sm:inline">{modes[m].label}</span>
          </label>
        {/each}
      </div>
      <p class="mt-1.5 text-[12px] text-faint">{modes[mode].hint}</p>
    </fieldset>

    {#if mode !== "anonymous"}
      <label class="mb-3 block">
        <span class="mb-1 block text-[12px] font-medium text-muted">{mode === "simple" ? "Username or bind DN" : "Bind DN"}</span>
        <input
          class="input font-mono"
          bind:value={username}
          name="username"
          autocomplete="username"
          placeholder="cn=admin,dc=example,dc=org"
          spellcheck="false"
          required
        />
      </label>
    {/if}

    {#if mode === "simple"}
      <label class="mb-3 block">
        <span class="mb-1 block text-[12px] font-medium text-muted">Password</span>
        <input class="input" type="password" bind:value={password} name="password" autocomplete="current-password" required />
      </label>
    {/if}

    <button type="button" class="mb-2 flex items-center gap-1 text-[12px] font-medium text-muted hover:text-ink" onclick={() => (advanced = !advanced)}>
      <ChevronDown class="size-3.5 transition-transform {advanced ? '' : '-rotate-90'}" />
      Options
    </button>

    {#if advanced}
      <div class="mb-3 space-y-2.5 rounded-lg border border-line p-3">
        <label class="block">
          <span class="mb-1 block text-[12px] font-medium text-muted">Base DN</span>
          <input class="input font-mono" bind:value={baseDN} placeholder={selected?.base_dn || "from the server's naming contexts"} spellcheck="false" />
        </label>
        {#if custom}
          <label class="flex items-center gap-2"><input type="checkbox" bind:checked={startTLS} /> StartTLS</label>
          <label class="flex items-center gap-2"><input type="checkbox" bind:checked={skipVerify} /> Skip TLS certificate verification</label>
          {#if !skipVerify}
            <div>
              <div class="mb-1 flex items-center justify-between text-[12px] font-medium text-muted">
                <span>CA certificate <span class="font-normal text-faint">(PEM, added to the system CAs)</span></span>
                <label class="cursor-pointer font-normal text-accent-text hover:underline">
                  Choose file<input type="file" accept=".pem,.crt,.cer,.ca-bundle,text/plain" class="sr-only" onchange={loadCA} />
                </label>
              </div>
              <textarea class="input h-auto min-h-16 resize-y py-1.5 font-mono text-[11.5px] leading-4" rows="3" bind:value={ca} placeholder="-----BEGIN CERTIFICATE-----" spellcheck="false"></textarea>
              {#if ca.trim() && !hasCA}<p class="mt-1 text-[12px] text-danger">No "BEGIN CERTIFICATE" block found.</p>{/if}
              {#if hasCA && !tlsInUse}<p class="mt-1 text-[12px] text-faint">Used only with ldaps:// or StartTLS.</p>{/if}
            </div>
          {:else}
            <p class="text-[12px] text-danger">The server certificate is not checked; anyone in between can read the password.</p>
          {/if}
        {:else if selected?.start_tls}
          <p class="text-[12px] text-faint">This server uses StartTLS.</p>
        {/if}
      </div>
    {/if}

    {#if error}
      <div class="mb-3 flex items-start gap-2 rounded-md bg-danger-soft px-3 py-2 text-danger" role="alert">
        <CircleAlert class="mt-0.5 size-4 shrink-0" />
        <span class="min-w-0 break-words">{error}</span>
      </div>
    {/if}

    <button class="btn btn-primary h-9 w-full justify-center" disabled={busy}>
      {#if busy}<LoaderCircle class="size-4 animate-spin" />{/if}
      Connect
    </button>

    <!-- last in the DOM so Tab starts at the form; placed in the corner visually -->
    <ThemeToggle class="absolute top-3 right-3" />
  </form>
</div>
