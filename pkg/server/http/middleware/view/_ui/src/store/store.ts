import { writable } from "svelte/store";

const config: Config = {
  grpc: [],
  swagger: [],
  swagger_settings: {}
}

type Config = {
  grpc: GRPC[];
  swagger: Swagger[];
  swagger_settings: SwaggerSettings;
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
