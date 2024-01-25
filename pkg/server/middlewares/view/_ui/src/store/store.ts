import { writable } from "svelte/store";

const config: Config = {
  swagger: {
    "Swagger Petstore": {
      link: "https://petstore.swagger.io/v2/swagger.json",
      schemes: ["https"],
      basePath: "/v2",
    },
    "Swagger Petstore HTTP": {
      link: "http://petstore.swagger.io/v2/swagger.json",
      schemes: ["http"],
      basePath: "/v2",
      basePathPrefix: "/api",
    },
  }
}

type Config = {
  swagger: Record<string, Swagger>
}

type Swagger = {
  link: string;
  schemes?: string[];
  host?: string;
  basePath?: string;
  basePathPrefix?: string;
  disableAuthorizeButton?: boolean;
}

export const storeInfo = writable(config);
export const storeTrigger = writable(false);
