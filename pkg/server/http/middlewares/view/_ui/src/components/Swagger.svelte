<script lang="ts">
  import axios, { AxiosError } from "axios";
  import { storeInfo } from "@/store/store";
  import SwaggerUI from "swagger-ui";
  import "swagger-ui/dist/swagger-ui.css";
  import update from "immutability-helper";

  let swaggerNode: HTMLDivElement;
  let msg = "";
  let isErr = false;

  const disableAuthorizeButton = function () {
    return {
      wrapComponents: {
        authorizeBtn: () => () => null,
      },
    };
  };

  export let params = {};

  const setMsg = (m: string, err: boolean) => {
    msg = m;
    isErr = err;
  };

  const clearMsg = () => {
    msg = "";
    isErr = false;
  };

  const swaggerGet = async (
    params: Record<string, string> | undefined,
    _: any,
  ) => {
    const service = params?.service;
    if (!service) {
      console.log("no service");
      return;
    }

    setMsg(`Loading '${service}'...`, false);

    // get link from store
    const swag = $storeInfo.swagger.find((s: any) => s.name === service);

    if (!swag) {
      setMsg(`Service '${service}' not found`, true);
      return;
    }

    try {
      const response = await axios.get(swag.link);
      let swaggerData = response.data;
      // console.log(swaggerData);

      if (swag.schemes) {
        swaggerData = update(swaggerData, {
          schemes: { $set: swag.schemes },
        });
      } else if ($storeInfo.swagger_settings.schemes) {
        swaggerData = update(swaggerData, {
          schemes: { $set: $storeInfo.swagger_settings.schemes },
        });
      }

      if (swag.host) {
        swaggerData = update(swaggerData, {
          host: { $set: swag.host },
        });
      } else if ($storeInfo.swagger_settings.host) {
        swaggerData = update(swaggerData, {
          host: { $set: $storeInfo.swagger_settings.host },
        });
      }

      if (swag.base_path) {
        swaggerData = update(swaggerData, {
          basePath: { $set: swag.base_path },
        });
      } else if ($storeInfo.swagger_settings.base_path) {
        swaggerData = update(swaggerData, {
          basePath: { $set: $storeInfo.swagger_settings.base_path },
        });
      }

      if (swag.base_path_prefix) {
        swaggerData = update(swaggerData, {
          basePath: { $set: `${swag.base_path_prefix}${swaggerData.basePath}` },
        });
      } else if ($storeInfo.swagger_settings.base_path_prefix) {
        swaggerData = update(swaggerData, {
          basePath: {
            $set: `${$storeInfo.swagger_settings.base_path_prefix}${swaggerData.basePath}`,
          },
        });
      }

      let plugins = [];
      if (swag.disable_authorize_button) {
        plugins.push(disableAuthorizeButton);
      } else if (
        swag.disable_authorize_button == null &&
        $storeInfo.swagger_settings.disable_authorize_button
      ) {
        plugins.push(disableAuthorizeButton);
      }

      SwaggerUI({
        domNode: swaggerNode,
        spec: swaggerData,
        plugins: plugins,
      });

      clearMsg();
    } catch (err) {
      setMsg((err as AxiosError).message, true);
    }
  };

  $: swaggerGet(params, $storeInfo);
</script>

<div class={!!msg ? "" : "hidden"}>
  <p class={`block p-2 text-white ${isErr ? "bg-red-500" : "bg-green-500"}`}>
    {msg}
  </p>
</div>

<div bind:this={swaggerNode} class={!!msg ? "hidden" : ""} />
