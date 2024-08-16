import { writable } from "svelte/store";

const config: Config = {
  iframe: [],
  page: [],
  grpc: [],
  swagger: [],
  swagger_settings: {}
}

type Config = {
  iframe: Iframe[];
  page: Page[];
  grpc: GRPC[];
  swagger: Swagger[];
  swagger_settings: SwaggerSettings;
}

type Page = {
  name: string;
  path: string;
}

type Iframe = {
  name: string;
  path: string;
  url: string;
}

type GRPC = {
  name: string;
}

type Swagger = {
  name: string;
  link: string;
  schemes?: string[];
  host?: string;
  base_path?: string;
  base_path_prefix?: string;
  disable_authorize_button?: boolean;
}

type SwaggerSettings = {
  schemes?: string[];
  host?: string;
  base_path?: string;
  base_path_prefix?: string;
  disable_authorize_button?: boolean;
}

export const storeInfo = writable(config);
export const storeTrigger = writable(false);
