<script lang="ts">
  import axios from "axios";
  import { formToObject } from "./helper/form";
  import { login } from "./helper/login";
  import { getRedirectPath, isResponseTypeCode } from "./helper/query";
  import { onMount } from "svelte";
  import Cookies from "js-cookie";
  import type { AuthInfo, Provider } from "./helper/info";

  let error = "";
  let working = false;
  let mounted = false;
  let togglePassword = false;

  let authInfo: AuthInfo = {
    title: "Login",
    provider: {
      password: [],
      code: [],
    } as Provider,
  };

  const togglepasswordFunc = () => {
    togglePassword = !togglePassword;
    const password = document.getElementById("password") as HTMLInputElement;
    if (togglePassword) {
      password.type = "text";
    } else {
      password.type = "password";
    }
  };

  let providerSelected = "";

  const signin = async (
    e: SubmitEvent & { currentTarget: EventTarget & HTMLFormElement },
  ) => {
    e.preventDefault();
    // prevent multiple click
    if (working) {
      return;
    }

    working = true;
    const data = formToObject(e.currentTarget);
    try {
      const url = authInfo.provider.password.find(
        (v) => v.name == providerSelected,
      )?.url;
      if (url == undefined) {
        throw new Error("Provider not found");
      }

      await login(url, data);

      // redirect to home
      if (!isResponseTypeCode()) {
        window.location.assign(getRedirectPath());

        return;
      }

      window.location.replace(window.location.href);

      return;
    } catch (reason: unknown) {
      if (axios.isAxiosError(reason)) {
        if (reason?.response?.status === 401) {
          error = "Invalid username or password";
        } else {
          error = reason?.response?.data?.error ?? reason.message;
        }
      } else {
        error = reason as any;
      }
    } finally {
      working = false;
    }
  };

  const info = async () => {
    try {
      const { data } = await axios.get<AuthInfo>(
        `./${import.meta.env.VITE_API}?auth_info=true`,
      );
      if (data.provider.password?.length > 0) {
        providerSelected = data.provider.password[0].name;
      }

      authInfo = data;
    } catch (reason: unknown) {
      let errorLog = "";
      if (axios.isAxiosError(reason)) {
        errorLog = reason?.response?.data?.error ?? reason.message;
      } else {
        errorLog = reason as any;
      }

      console.error(errorLog);
    }
  };

  const checkWindow = (url: string) => {
    var win = window.open(url);

    var timer = setInterval(() => {
      if (win?.closed) {
        clearInterval(timer);
        // read cookie of auth_verify
        const v = Cookies.get("auth_verify");
        if (v == "true") {
          if (!isResponseTypeCode()) {
            // redirect to home
            window.location.assign(getRedirectPath());

            return;
          }

          window.location.replace(window.location.href);
        }
      }
    }, 500);
  };

  onMount(async () => {
    await info();

    // if query has title
    const title = new URLSearchParams(window.location.search).get("title");
    if (title) {
      authInfo.title = title;
    }

    // change header title
    document.title = authInfo.title;

    if (!!authInfo.error) {
      error = authInfo.error;
    }

    mounted = true;
  });
</script>

<div class="w-full min-h-screen bg-gray-50 flex flex-col items-center sm:pt-6">
  <div class="w-full sm:max-w-md sm:p-5 mx-auto">
    <div class="border p-4 bg-white relative sm:rounded-md">
      <h2 class="mb-2 text-xl font-bold [line-height:1.2]">
        <span class={mounted ? "" : "invisible"}>{authInfo.title}</span>
      </h2>
      <hr class="mb-2" />
      {#if authInfo.provider.password?.length}
        {#if authInfo.provider.password?.length > 1}
          <div class="float-right">
            <select bind:value={providerSelected} class="px-2">
              {#each authInfo.provider.password as provider}
                <option value={provider.name}>
                  {provider.name}
                </option>
              {/each}
            </select>
          </div>
        {/if}

        <form on:submit|preventDefault|stopPropagation={signin}>
          <div class="mb-4">
            <label class="block mb-1" for="username">Username</label>
            <input
              id="username"
              type="text"
              name="username"
              class="py-1.5 px-3 border rounded-md border-gray-300 focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full"
            />
          </div>
          <div class="mb-4">
            <label class="block mb-1" for="password">Password</label>
            <div class="relative">
              <input
                id="password"
                type="password"
                name="password"
                autocomplete="off"
                class="py-1.5 px-3 border rounded-md border-gray-300 focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full"
              />
              <button
                type="button"
                on:click={togglepasswordFunc}
                class="absolute inset-y-0 end-0 flex items-center z-20 px-3 cursor-pointer text-gray-400 rounded-e-md focus:outline-none focus:text-blue-600 dark:text-neutral-600 dark:focus:text-blue-500"
              >
                <svg
                  class="shrink-0 size-3.5"
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                >
                  <path
                    class={togglePassword ? "hidden" : ""}
                    d="M9.88 9.88a3 3 0 1 0 4.24 4.24"
                  ></path>
                  <path
                    class={togglePassword ? "hidden" : ""}
                    d="M10.73 5.08A10.43 10.43 0 0 1 12 5c7 0 10 7 10 7a13.16 13.16 0 0 1-1.67 2.68"
                  ></path>
                  <path
                    class={togglePassword ? "hidden" : ""}
                    d="M6.61 6.61A13.526 13.526 0 0 0 2 12s3 7 10 7a9.74 9.74 0 0 0 5.39-1.61"
                  ></path>
                  <line
                    class={togglePassword ? "hidden" : ""}
                    x1="2"
                    x2="22"
                    y1="2"
                    y2="22"
                  ></line>
                  <path
                    class={togglePassword ? "block" : "hidden"}
                    d="M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z"
                  ></path>
                  <circle
                    class={togglePassword ? "block" : "hidden"}
                    cx="12"
                    cy="12"
                    r="3"
                  ></circle>
                </svg>
              </button>
            </div>
          </div>
          <div class="mt-6">
            <button
              type="submit"
              class="block w-full text-center px-4 py-1.5 bg-[#615fff] border rounded-md border-transparent font-semibold capitalize text-white hover:bg-blue-500 active:bg-blue-500 focus:outline-none focus:border-blue-500 focus:ring focus:ring-blue-200 disabled:bg-gray-400 transition"
              disabled={working}
            >
              Sign in
            </button>
          </div>
        </form>
      {/if}
      {#if authInfo.provider.code?.length}
        {#if authInfo.provider.password?.length}
          <hr class="mt-8 mb-6 custom-hr" />
        {/if}
        {#each authInfo.provider.code as provider}
          <button
            title={provider.url}
            on:click={async () => {
              checkWindow(provider.url);
            }}
            class="block rounded-md mt-1 w-full text-center px-4 py-1.5 bg-white border border-gray-300 font-semibold capitalize text-black hover:bg-gray-50 active:bg-blue-50 focus:outline-none focus:border-blue-500 focus:ring focus:ring-blue-200 disabled:bg-gray-400 transition"
          >
            {provider.name}
          </button>
        {/each}
      {/if}
      {#if error != ""}
        <div class="mt-4 px-0.5 bg-red-200">
          <span class="break-all">{error}</span>
        </div>
      {/if}
    </div>
  </div>
</div>

<style lang="scss">
  .custom-hr {
    @apply text-black text-center overflow-visible;

    &::after {
      content: "Or continue with";
      top: -13px;

      @apply bg-white relative px-2;
    }
  }
</style>
