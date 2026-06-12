<script lang="ts">
  export let label: string;
  export let value: number;
  export let index = "00";
</script>

<div class="relative bg-crt p-4">
  <span class="absolute right-2 top-2 text-[10px] text-dim">{index}</span>
  <p class="font-display text-4xl leading-none tracking-tight text-fg md:text-5xl">
    {String(value).padStart(2, "0")}
  </p>
  <p class="t-label mt-3">[ {label} ]</p>
</div>
