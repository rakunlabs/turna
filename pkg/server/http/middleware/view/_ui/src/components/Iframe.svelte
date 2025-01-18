<script lang="ts">
  import { storeInfo, type Group, type Iframe } from "@/store/store";
  import { onMount } from "svelte";

  export let params: Record<string, string> | undefined = {};

  let src = "";
  let error = "";

  const visitService = (
    checks: string[],
    groups: Group[] | undefined,
  ): Iframe | undefined => {
    let iframe: Iframe | undefined = undefined;
    for (let group of groups || []) {
      if (group.name !== checks[0]) {
        continue;
      }

      for (let s of group.services || []) {
        if (s.name !== checks[1]) {
          continue;
        }

        iframe = s.iframe?.find((s: any) => s.path === checks[2]);
        if (iframe) {
          break;
        }
      }

      if (iframe) {
        break;
      }

      if (group.groups) {
        iframe = visitService(checks.slice(1), group.groups);
        if (iframe) {
          break;
        }
      }
    }

    return iframe;
  };

  const lookIframe = (service: string) => {
    // split service with /
    let checks = service.split("/");

    if (checks.length == 1) {
      return $storeInfo.iframe?.find((s: any) => s.path === service);
    }

    // look in groups use for loop and break
    let iframe = visitService(checks, $storeInfo.groups);

    return iframe;
  };

  onMount(() => {
    let iframe = lookIframe(params?.wild ?? "");

    // console.log(iframe);

    if (iframe) {
      error = "";
      src = iframe.url ?? "";
    } else {
      error = `Iframe '${params?.wild}' not found`;
    }
  });
</script>

{#if error.length > 0}
  <div>
    <p class="block py-1 px-2 text-white bg-red-500">
      {error}
    </p>
  </div>
{:else}
  <iframe class="w-full h-full" {src} title={params?.wild} />
{/if}
