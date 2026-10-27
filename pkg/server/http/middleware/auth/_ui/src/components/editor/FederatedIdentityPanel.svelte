<script lang="ts">
  import { onMount } from "svelte";

  import Section from "../ui/Section.svelte";
  import BreakSeal from "../ui/BreakSeal.svelte";
  import { docket, messageOf, session } from "../../lib/state/session.svelte";
  import { formatStamp } from "../../lib/records";

  /**
   * Upstream (OAuth2/SAML) accounts bound to this user. A federated login
   * signs in as the user its provider+subject was first linked to; unlinking
   * lets the next login through that account be resolved again.
   */
  let { userID }: { userID: string } = $props();

  type Identity = {
    provider: string;
    subject: string;
    user_id: string;
    alias: string;
    created_at: string;
    last_login_at: string;
  };

  let identities = $state<Identity[]>([]);
  let working = $state(false);
  let surveyed = $state(false);
  let loadFailed = $state(false);
  let pendingRemoval = $state("");

  const base = $derived(`users/${encodeURIComponent(userID)}/federated-identities`);
  const keyOf = (identity: Identity) => `${identity.provider}\u0000${identity.subject}`;

  async function load() {
    try {
      const res = await session.request<Identity[]>(base);
      identities = res.payload ?? [];
      loadFailed = false;
      surveyed = true;
    } catch (err) {
      loadFailed = true;
      docket.reject(messageOf(err, "Cannot read this user's linked accounts"));
    }
  }

  async function unlink(identity: Identity) {
    pendingRemoval = "";
    working = true;
    try {
      const query = new URLSearchParams({ provider: identity.provider, subject: identity.subject });
      await session.request(`${base}?${query}`, { method: "DELETE" });
      await load();
      docket.commit(`${identity.provider} account unlinked`);
    } catch (err) {
      docket.reject(messageOf(err, "Cannot unlink this account"));
    } finally {
      working = false;
    }
  }

  onMount(() => {
    void load();
  });
</script>

<Section
  title="Linked accounts"
  note="Upstream accounts this user signs in with. A login is bound by the provider's stable account id, so it keeps resolving here even if the email or username changes upstream."
>
  {#snippet aside()}
    <span class="stamp">{identities.length} linked</span>
  {/snippet}

  {#if loadFailed && !surveyed}
    <div class="border border-dashed border-seal/60 px-6 py-8 text-center" role="alert">
      <p class="text-[13px] leading-[1.6] text-ink">This user's linked accounts could not be read.</p>
      <button type="button" class="act mt-4" disabled={working} onclick={() => void load()}>Try again</button>
    </div>
  {:else if !surveyed}
    <p class="text-[13px] text-muted">Reading linked accounts…</p>
  {:else if identities.length === 0}
    <p class="border border-dashed border-rule px-6 py-10 text-center text-[13px] leading-[1.6] text-muted">
      No upstream account is linked. The first OAuth2 or SAML login that resolves to this user links itself.
    </p>
  {:else}
    <ul class="border border-rule bg-sheet">
      {#each identities as identity (keyOf(identity))}
        <li class="border-b border-rule last:border-b-0">
          <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3">
            <div class="min-w-0">
              <p class="truncate text-[13.5px] font-medium text-ink">
                {identity.provider} · {identity.alias || identity.subject}
              </p>
              <p class="serial mt-0.5 truncate text-[12px] text-muted">
                subject {identity.subject} · linked {formatStamp(identity.created_at) || "—"} · last login
                {formatStamp(identity.last_login_at) || "—"}
              </p>
            </div>

            <button
              type="button"
              class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
              aria-expanded={pendingRemoval === keyOf(identity)}
              disabled={working}
              onclick={() => (pendingRemoval = pendingRemoval === keyOf(identity) ? "" : keyOf(identity))}
            >
              Unlink
            </button>
          </div>

          {#if pendingRemoval === keyOf(identity)}
            <div class="px-4 pb-4">
              <BreakSeal
                consequence={`Unlinking removes the binding to this ${identity.provider} account. Its next login is matched by alias again — it may land on this user, on another user with that alias, or be refused. Use it when the person's upstream account was replaced.`}
                action="Unlink this account"
                disabled={working}
                onconfirm={() => void unlink(identity)}
              />
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</Section>
