<script lang="ts">
  import { joinValues, splitValues } from "../../lib/records";
  import HeldEntry from "./HeldEntry.svelte";

  /** A list typed as text: comma separated on one line, or one per line. */
  let {
    id,
    value,
    onchange,
    placeholder = "",
    describedby = undefined,
    multiline = false,
    invalid = false,
    class: extra = "",
  }: {
    id: string;
    value: unknown;
    onchange: (items: string[]) => void;
    placeholder?: string;
    describedby?: string;
    multiline?: boolean;
    invalid?: boolean;
    class?: string;
  } = $props();

  const text = $derived(
    multiline && Array.isArray(value) ? value.map(String).join("\n") : joinValues(value),
  );
</script>

<HeldEntry
  {id}
  {placeholder}
  {describedby}
  {multiline}
  {invalid}
  class={multiline ? `min-h-24 ${extra}` : extra}
  value={text}
  onvalue={(next) => onchange(splitValues(next))}
/>
