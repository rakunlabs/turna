import { get, writable } from "svelte/store";

let navbar = {
  title: "",
  sideBarOpen: false
};

let apiPrefix = ""

export const storeNavbar = writable(navbar);
export const storeApiPrefix = writable(apiPrefix);

export const getApiPrefix = () => {
  let prefix = get(storeApiPrefix)
  if (prefix === "") {
    return "."
  }

  if (prefix.endsWith("/")) {
    return prefix.slice(0, -1)
  }

  return prefix
}
