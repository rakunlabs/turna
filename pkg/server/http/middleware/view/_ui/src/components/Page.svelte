<script lang="ts">
  export let params: Record<string, string> | undefined = {};

  let msg = "Loading...";

  const loadFinish = () => {
    msg = "";
  };
</script>

<div class={!!msg ? "" : "hidden"}>
  <p class={`block py-1 px-2 text-white bg-indigo-500`}>
    {msg}
  </p>
</div>

<iframe
  class="w-full h-full"
  src="./page/{params?.wild}"
  title={params?.wild}
  on:load={loadFinish}
/>
