<script lang="ts">
  import axios from "axios";
  import { formToObject } from "./helper/form";
  import { login } from "./helper/login";
  import { getRedirectPath } from "./helper/query";
  import { onMount } from "svelte";
  import Cookies from "js-cookie";
  import type { AuthInfo, Provider } from "./helper/info";

  let error = "";
  let working = false;
  let mounted = false;

  let authInfo: AuthInfo = {
    title: "Login",
    provider: {
      password: [],
      code: [],
    } as Provider,
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
      window.location.assign(getRedirectPath());
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
          // redirect to home
          window.location.assign(getRedirectPath());
        }
      }
    }, 500);
  };

  onMount(async () => {
    await info();
    mounted = true;
  });
</script>

<div
  class="w-full min-h-screen bg-gray-50 flex flex-col items-center pt-6 sm:pt-0 bg-image"
>
  <div class="w-full sm:max-w-md p-5 mx-auto">
    <div class="border p-4 bg-yellow-50 relative">
      <h2 class="mb-2 text-xl font-bold [line-height:1.2]">
        <span class={mounted ? "" : "invisible"}>{authInfo.title}</span>
      </h2>
      <hr class="mb-2" />
      {#if authInfo.provider.password?.length}
        <div class="float-right">
          <select bind:value={providerSelected} class="px-2">
            {#each authInfo.provider.password as provider}
              <option value={provider.name}>
                {provider.name}
              </option>
            {/each}
          </select>
        </div>

        <form on:submit|preventDefault|stopPropagation={signin}>
          <div class="mb-4">
            <label class="block mb-1" for="username">Username</label>
            <input
              id="username"
              type="text"
              name="username"
              class="py-2 px-3 border border-gray-300 focus:border-red-300 focus:outline-none focus:ring focus:ring-red-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full"
            />
          </div>
          <div class="mb-4">
            <label class="block mb-1" for="password">Password</label>
            <input
              id="password"
              type="password"
              name="password"
              class="py-2 px-3 border border-gray-300 focus:border-red-300 focus:outline-none focus:ring focus:ring-red-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full"
            />
          </div>
          <div class="mt-6">
            <button
              type="submit"
              class="block w-full text-center px-4 py-2 bg-red-400 border border-transparent font-semibold capitalize text-white hover:bg-red-500 active:bg-red-500 focus:outline-none focus:border-red-500 focus:ring focus:ring-red-200 disabled:bg-gray-400 transition"
              disabled={working}
            >
              Login
            </button>
          </div>
        </form>
      {/if}
      {#if authInfo.provider.code?.length}
        {#if authInfo.provider.password?.length}
          <hr class="my-2" />
        {/if}
        {#each authInfo.provider.code as provider}
          <button
            title={provider.url}
            on:click={async () => {
              checkWindow(provider.url);
            }}
            class="block mt-1 w-full text-center px-4 py-2 bg-sky-400 border border-transparent font-semibold capitalize text-white hover:bg-sky-500 active:bg-sky-500 focus:outline-none focus:border-sky-500 focus:ring focus:ring-red-200 disabled:bg-gray-400 transition"
          >
            Login with {provider.name}
          </button>
        {/each}
      {/if}
      {#if error != ""}
        <div class="mt-4 bg-red-200">
          <span class="break-all">{error}</span>
        </div>
      {/if}
    </div>
  </div>
</div>

<style lang="scss">
  .bg-image {
    background-image: url('data:image/svg+xml,<svg id="patternId" width="100%" height="100%" xmlns="http://www.w3.org/2000/svg"><defs><pattern id="a" patternUnits="userSpaceOnUse" width="40" height="80" patternTransform="scale(2) rotate(0)"><rect x="0" y="0" width="100%" height="100%" fill="hsla(0, 0%, 99%, 1)"/><path d="M-10 7.5l20 5 20-5 20 5" stroke-linecap="square" stroke-width="1" stroke="hsla(54, 100%, 92%, 1)" fill="none"/><path d="M-10 27.5l20 5 20-5 20 5" stroke-linecap="square" stroke-width="1" stroke="hsla(54, 0%, 96%, 1)" fill="none"/><path d="M-10 47.5l20 5 20-5 20 5" stroke-linecap="square" stroke-width="1" stroke="hsla(199, 100%, 94%, 1)" fill="none"/><path d="M-10 67.5l20 5 20-5 20 5" stroke-linecap="square" stroke-width="1" stroke="hsla(4, 100%, 90%, 1)" fill="none"/></pattern></defs><rect width="800%" height="800%" transform="translate(0,0)" fill="url(%23a)"/></svg>');
  }
</style>
