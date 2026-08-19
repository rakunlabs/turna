<script lang="ts">
  export let label: string;
  export let value: number;
</script>

<div class="rounded-lg border border-line bg-surface p-4 shadow-sm">
  <p class="font-display text-3xl leading-none tracking-tight text-fg md:text-4xl">
    {value}
  </p>
  <p class="t-label mt-2">{label}</p>
</div>
