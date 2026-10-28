<script lang="ts">
  export let checked = false;
</script>

<label class="flex cursor-pointer select-none items-center gap-2.5 text-sm" for="remember-me">
  <input
    id="remember-me"
    type="checkbox"
    bind:checked
    class="h-4 w-4 shrink-0 rounded border-line bg-field text-brand focus:ring-2 focus:ring-brand/30"
  />
  <span class="font-medium text-ink">Keep me signed in</span>
</label>
