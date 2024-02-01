import { writable } from "svelte/store";

const config: Config = {
  swagger: [],
  swagger_settings: {}
}

type Config = {
  swagger: Swagger[];
  swagger_settings: SwaggerSettings;
}

type Swagger = {
  name: string;
  link: string;
  schemes?: string[];
  host?: string;
  basePath?: string;
  basePathPrefix?: string;
  disableAuthorizeButton?: boolean;
}

type SwaggerSettings = {
  schemes?: string[];
  host?: string;
  basePath?: string;
  basePathPrefix?: string;
  disableAuthorizeButton?: boolean;
}

export const storeInfo = writable(config);
export const storeTrigger = writable(false);
