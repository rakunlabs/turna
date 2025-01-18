import { writable } from "svelte/store";

const config: Config = {
  home: {},

  iframe: [],
  page: [],
  grpc: [],
  swagger: [],
  swagger_settings: {},

  groups: [],
}

type Config = {
  home?: Home;

  iframe?: Iframe[];
  page?: Page[];
  grpc?: GRPC[];
  swagger?: Swagger[];
  swagger_settings?: SwaggerSettings;

  groups?: Group[];
}

type Group = {
  name: string;
  services?: Service[];

  groups?: Group[];
}

type Service = {
  name: string;

  iframe?: Iframe[];
  page?: Page[];
  grpc?: GRPC[];
  swagger?: Swagger[];
  swagger_settings?: SwaggerSettings;
}

type Page = {
  name?: string;
  path?: string;
  path_extra?: string;
}

type Iframe = {
  name?: string;
  path?: string;
  url?: string;
}

type GRPC = {
  name: string;
}

type Swagger = {
  name?: string;
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

type Home = {
  type?: string;
  content?: string;
}

export const storeInfo = writable(config);
export const storeTrigger = writable(false);

export type {
  Service,
  Group,
  Swagger,
  Iframe,
  GRPC,
  Page,
}
