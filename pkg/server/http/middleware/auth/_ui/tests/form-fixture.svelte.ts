import type { AnyRecord, SettingNamespace } from "../src/lib/api";
import { cloneRecord } from "../src/lib/records";
import { syncSettingForm } from "../src/lib/state/setting-form.svelte";

/** Exercise the same client-side runes/effects as row-based settings pages. */
export class FormFixture {
  draft = $state<AnyRecord>({});
  destroy: () => void;

  constructor(namespace: SettingNamespace) {
    this.destroy = $effect.root(() => {
      syncSettingForm(namespace, () => cloneRecord(this.draft), (record) => { this.draft = cloneRecord(record); });
    });
  }
}
