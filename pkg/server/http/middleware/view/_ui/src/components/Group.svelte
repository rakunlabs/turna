<script lang="ts">
  import { location } from "svelte-spa-router";
  import { type Group } from "@/store/store";
  import Link from "./Link.svelte";
  import Toggle from "./Toggle.svelte";

  export let groups: Group[] | undefined = undefined;
  export let path: string = "";
</script>

{#each groups || [] as group}
  <div>
    <Toggle name={group.name} class="bg-yellow-100" toggle={true}>
      <div class="border-l-4 border-gray-600">
        <div>
          {#each group.services || [] as service}
            <Toggle
              name={service.name}
              class="bg-white hover:bg-gray-100"
              toggleCheck={() => {
                for (let base of ["/swagger", "/grpc", "/page", "/iframe"]) {
                  if (
                    $location.startsWith(
                      base + path + "/" + group.name + "/" + service.name
                    )
                  ) {
                    return true;
                  }
                }

                return false;
              }}
            >
              {#each service.swagger || [] as swagger}
                <Link
                  path={path +
                    "/" +
                    group.name +
                    "/" +
                    service.name +
                    "/" +
                    (swagger.name ?? "swagger")}
                  name={swagger.name}
                  icon={true}
                  type="swagger"
                />
              {/each}
              {#each service.grpc || [] as grpc}
                <Link
                  path={path +
                    "/" +
                    group.name +
                    "/" +
                    service.name +
                    "/" +
                    (grpc.name ?? "grpc")}
                  name={grpc.name}
                  icon={true}
                  type="grpc"
                />
              {/each}
              {#each service.page || [] as page}
                <Link
                  path={path +
                    "/" +
                    group.name +
                    "/" +
                    service.name +
                    "/" +
                    ((page.path ?? "page") + (page.path_extra ?? ""))}
                  name={page.name}
                  icon={true}
                  type="page"
                />
              {/each}
              {#each service.iframe || [] as iframe}
                <Link
                  path={path +
                    "/" +
                    group.name +
                    "/" +
                    service.name +
                    "/" +
                    (iframe.path ?? "iframe")}
                  name={iframe.name}
                  icon={true}
                  type="iframe"
                />
              {/each}
            </Toggle>
          {/each}
        </div>
        <svelte:self groups={group.groups} path={path + "/" + group.name} />
      </div>
    </Toggle>
  </div>
{/each}
