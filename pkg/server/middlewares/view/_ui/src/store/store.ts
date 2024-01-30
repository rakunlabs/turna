import { writable } from "svelte/store";

const config: Config = {
  swagger: {}
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
