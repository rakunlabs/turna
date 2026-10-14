<script lang="ts">
  import type { HTMLInputAttributes } from "svelte/elements";

  /**
   * A text control whose stored value is normalised by its owner (split into a
   * list, parsed as a number, defaulted). While the field has focus the
   * operator's own text is kept, so a trailing comma, a cleared number or a
   * half-typed entry is never rewritten under the cursor; once the field is
   * left it shows the stored value again.
   */
  let {
    id,
    value,
    onvalue,
    placeholder = "",
    describedby = undefined,
    multiline = false,
    mono = true,
    invalid = false,
    type = "text",
    min = undefined,
    rows = undefined,
    disabled = false,
    autocomplete = "off",
    class: extra = "",
  }: {
    id: string;
    value: string;
    onvalue: (value: string) => void;
    placeholder?: string;
    describedby?: string;
    multiline?: boolean;
    mono?: boolean;
    invalid?: boolean;
    type?: "text" | "number" | "password" | "email" | "url";
    min?: string | number;
    rows?: number;
    disabled?: boolean;
    autocomplete?: HTMLInputAttributes["autocomplete"];
    class?: string;
  } = $props();

  let draft = $state("");
  let focused = false;
  /** What the owner stored for the last keystroke; anything else is an outside change. */
  let echo: string | undefined;

  $effect.pre(() => {
    const next = value;
    if (!focused || next !== echo) draft = next;
  });

  function onfocus() {
    focused = true;
  }

  function onblur() {
    focused = false;
    echo = undefined;
    draft = value;
  }

  function oninput(event: Event & { currentTarget: HTMLInputElement | HTMLTextAreaElement }) {
    draft = event.currentTarget.value;
    onvalue(draft);
    echo = value;
  }
</script>

{#if multiline}
  <textarea
    {id}
    {placeholder}
    {rows}
    {disabled}
    class="exhibit mt-1.5 {extra}"
    spellcheck="false"
    autocomplete="off"
    aria-describedby={describedby}
    aria-invalid={invalid || undefined}
    style={invalid ? "border-color: var(--w-seal)" : undefined}
    value={draft}
    {onfocus}
    {onblur}
    {oninput}
  ></textarea>
{:else}
  <input
    {id}
    {type}
    {min}
    {placeholder}
    {disabled}
    {autocomplete}
    class="entry mt-1.5 {mono ? 'serial' : ''} {extra}"
    spellcheck="false"
    aria-describedby={describedby}
    aria-invalid={invalid || undefined}
    style={invalid ? "border-bottom-color: var(--w-seal)" : undefined}
    value={draft}
    {onfocus}
    {onblur}
    {oninput}
  />
{/if}
