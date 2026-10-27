import { untrack } from "svelte";
import type { AnyRecord, SettingNamespace } from "../api";
import { settings, settingRecord, setSettingRecord } from "./settings.svelte";

/**
 * Bridge row-based forms to the shared draft store. Reloads replace a clean
 * form, while edits survive unrelated writes and navigating to another page.
 * The serialized baseline avoids marking normalization on initial load dirty.
 */
export function syncSettingForm(
  namespace: SettingNamespace,
  readDraft: () => AnyRecord,
  applyRecord: (record: AnyRecord) => void,
) {
  let syncedFrom: AnyRecord | null = null;
  let signature = "";

  $effect(() => {
    const record = settingRecord(namespace);
    if (record === syncedFrom) return;
    untrack(() => {
      syncedFrom = record;
      applyRecord(record);
      const normalized = readDraft();
      signature = JSON.stringify(normalized);
      settings.normalizeFormBaseline(namespace, normalized);
      syncedFrom = settingRecord(namespace);
    });
  });

  $effect(() => {
    const draft = readDraft();
    const next = JSON.stringify(draft);
    if (next === signature) return;
    untrack(() => {
      signature = next;
      setSettingRecord(namespace, draft);
      syncedFrom = settingRecord(namespace);
    });
  });
}
