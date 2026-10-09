<script lang="ts">
  export let checked = false;
</script>

<label
  class="flex cursor-pointer select-none items-start gap-3 rounded-md py-1 text-sm"
  for="remember-me"
>
  <input
    id="remember-me"
    type="checkbox"
    bind:checked
    aria-describedby="remember-me-hint"
    class="mt-0.5 h-4 w-4 shrink-0 rounded border-line bg-field text-brand focus:ring-2 focus:ring-brand/30"
  />
  <span class="min-w-0">
    <span class="font-medium text-ink">Keep me signed in</span>
    <span id="remember-me-hint" class="block text-xs leading-5 text-muted">
      Stay signed in on this device while you keep using the site.
    </span>
  </span>
</label>
