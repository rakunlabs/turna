<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";

  /**
   * A field is a ruled line with an engraved label, the way an entry on a form
   * is filled in — not a rounded box floating on a card.
   */
  let {
    label,
    value = $bindable(""),
    hint = "",
    placeholder = "",
    type = "text",
    mono = false,
    disabled = false,
    invalid = false,
    autocomplete = "off",
    id: givenID,
  }: {
    label: string;
    value?: string;
    hint?: string;
    placeholder?: string;
    type?: "text" | "password" | "email" | "number" | "url" | "datetime-local";
    mono?: boolean;
    disabled?: boolean;
    invalid?: boolean;
    autocomplete?: HTMLInputAttributes["autocomplete"];
    id?: string;
  } = $props();

  // randomUUID exists only in secure contexts; $props.id() works over plain HTTP too.
  const fallbackID = $props.id();
  const id = $derived(givenID || fallbackID);

  const hintID = $derived(hint ? `${id}-hint` : undefined);

  // `value` is often a getter/setter bind whose setter normalises (lists,
  // numbers). Writing the normalised value back into a focused field would
  // eat commas and cleared digits under the cursor, so the typed text is held
  // until the field is left.
  let draft = $state("");
  let focused = false;
  let echo: string | undefined;

  $effect.pre(() => {
    const next = value;
    if (!focused || next !== echo) draft = next;
  });
</script>

<div class="min-w-0">
  <label class="stamp block" for={id}>{label}</label>
  <input
    {id}
    {type}
    {placeholder}
    {disabled}
    {autocomplete}
    aria-describedby={hintID}
    aria-invalid={invalid || undefined}
    class="entry mt-1.5 {mono ? 'serial' : ''}"
    style={invalid ? "border-bottom-color: var(--w-seal)" : undefined}
    value={draft}
    onfocus={() => (focused = true)}
    onblur={() => {
      focused = false;
      echo = undefined;
      draft = value;
    }}
    oninput={(event) => {
      draft = event.currentTarget.value;
      value = draft;
      echo = value;
    }}
  />
  {#if hint}
    <p id={hintID} class="mt-1.5 text-[12px] leading-[1.5] {invalid ? 'text-seal' : 'text-muted'}">
      {hint}
    </p>
  {/if}
</div>
