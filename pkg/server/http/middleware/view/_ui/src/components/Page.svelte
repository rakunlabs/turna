<script lang="ts">
  export let params: Record<string, string> | undefined = {};
</script>

<iframe
  class="w-full h-full"
  src="./page/{params?.name}/"
  title={params?.name}
/>
