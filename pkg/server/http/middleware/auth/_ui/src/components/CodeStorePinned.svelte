<script lang="ts">
  import ConfigPinned from "./ui/ConfigPinned.svelte";
  import { session } from "../lib/state/session.svelte";

  /** The code store this instance runs, as its static config pins it. */
  const pinned = $derived(session.instanceConfig?.cache.code_store);

  const rows = $derived.by(() => {
    if (!pinned?.pinned) return [];

    const out = [{ label: "Store", value: pinned.active ?? "" }];
    const redis = pinned.redis;
    if (pinned.active === "redis" && redis) {
      out.push(
        { label: "Addresses", value: (redis.address ?? []).join(", ") || "—" },
        { label: "Cluster mode", value: redis.cluster ? "forced" : "automatic" },
        { label: "Key prefix", value: pinned.key_prefix || "none (code_…)" },
        { label: "Username", value: redis.username || "—" },
        { label: "Password", value: redis.password_set ? "set in config" : "not set" },
        { label: "Client name", value: redis.client_name || "—" },
        { label: "TLS", value: redis.tls.enabled ? "enabled" : "disabled" },
      );
      if (redis.tls.enabled) {
        out.push(
          { label: "CA file", value: redis.tls.ca_file || "—" },
          { label: "Certificate file", value: redis.tls.cert_file || "—" },
          { label: "Key file", value: redis.tls.key_file || "—" },
        );
      }
    }

    return out;
  });
</script>

<ConfigPinned configKey="cache.code_store" {rows}>
  This instance ignores the stored code store and uses its config file instead; change it there and
  restart. Committing this page leaves the stored code store untouched for instances without an
  override.
</ConfigPinned>
