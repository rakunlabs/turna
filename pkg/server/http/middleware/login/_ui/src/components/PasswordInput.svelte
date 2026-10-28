<script lang="ts">
  export let id: string;
  export let name = "password";
  export let value = "";
  export let required = false;
  export let minlength: number | null = null;
  export let autocomplete: AutoFill = "current-password";
  export let placeholder = "";
  export let describedby: string | undefined = undefined;

  let show = false;
</script>

<div class="relative">
  <input
    {id}
    {name}
    {required}
    {placeholder}
    {autocomplete}
    minlength={minlength ?? undefined}
    aria-describedby={describedby}
    type={show ? "text" : "password"}
    {value}
    class="field pr-10"
  />
  <button
    type="button"
    aria-label="Show password"
    aria-pressed={show}
    aria-controls={id}
    on:click={() => (show = !show)}
    class="absolute inset-y-0 end-0 flex w-10 items-center justify-center rounded-e-md text-muted transition hover:text-ink focus:outline-none focus-visible:ring-2 focus-visible:ring-brand/40"
  >
    <svg
      class="shrink-0"
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      {#if show}
        <path d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"></path>
        <circle cx="12" cy="12" r="3"></circle>
      {:else}
        <path d="M9.88 9.88a3 3 0 1 0 4.24 4.24"></path>
        <path d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68"></path>
        <path d="M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61"></path>
        <line x1="2" x2="22" y1="2" y2="22"></line>
      {/if}
    </svg>
  </button>
</div>
