<script lang="ts">
  import axios from "axios";
  import { formToObject } from "./helper/form";
  import { login } from "./helper/login";
  import { getRedirectPath, isResponseTypeCode } from "./helper/query";
  import { onMount } from "svelte";
  import Cookies from "js-cookie";
  import type { AuthInfo, Provider } from "./helper/info";
  import { isWebAuthnSupported, startAuthentication } from "./helper/webauthn";
  import type { ServerRequestOptions } from "./helper/webauthn";
  import PasswordInput from "./components/PasswordInput.svelte";

  let error = "";
  let notice = "";
  let working = false;
  let mounted = false;

  type View = "signin" | "signup" | "verify" | "reset" | "reset-confirm";
  let view: View = "signin";
  let verifyCode = "";
  let resetCode = "";

  const inputClass =
    "py-1.5 px-3 border rounded-md border-gray-300 focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full";
  const submitClass =
    "block w-full text-center px-4 py-1.5 bg-[#615fff] border rounded-md border-transparent font-semibold capitalize text-white hover:bg-blue-500 active:bg-blue-500 focus:outline-none focus:border-blue-500 focus:ring focus:ring-blue-200 disabled:bg-gray-400 transition";
  const secondaryClass =
    "block w-full text-center px-4 py-1.5 bg-white border border-gray-300 rounded-md font-semibold text-black hover:bg-gray-50 active:bg-blue-50 focus:outline-none focus:border-blue-500 focus:ring focus:ring-blue-200 disabled:bg-gray-400 transition";
  const linkClass = "text-sm text-blue-600 hover:underline cursor-pointer bg-transparent border-0 p-0";

  let authInfo: AuthInfo = {
    title: "Login",
    provider: {
      password: [],
      code: [],
    } as Provider,
  };


  let providerSelected = "";

  $: passwordLink = authInfo.provider.password?.find((v) => v.name == providerSelected);
  $: canSignup = !!passwordLink?.signup_url;
  $: canReset = !!passwordLink?.password_reset_url;
  $: passwordMinLength = passwordLink?.password_min_length || 8;

  // credential-mismatch descriptions are collapsed into a single neutral
  // message so the UI never reveals whether the user or the password was wrong.
  const credentialErrors = ["password not match", "user not found", "secret not match"];

  // axiosError reads the standard {message, error} envelope used across the
  // auth/login backend, unwrapping any embedded oauth2 error body, and maps
  // credential failures to a friendly message.
  const axiosError = (reason: unknown) => {
    if (axios.isAxiosError(reason)) {
      const data: any = reason?.response?.data;
      let detail: string | undefined;
      if (data && typeof data === "object") {
        detail = data.message ?? data.error_description ?? data.error;
      } else if (typeof data === "string") {
        detail = data;
      }

      if (typeof detail === "string" && detail.trim().startsWith("{")) {
        try {
          const inner = JSON.parse(detail);
          detail = inner.error_description ?? inner.message ?? inner.error ?? detail;
        } catch {
          // keep detail as-is
        }
      }

      if (typeof detail === "string" && detail) {
        if (credentialErrors.includes(detail.toLowerCase())) {
          return "Invalid username or password";
        }

        return detail;
      }

      if (reason?.response?.status === 401) {
        return "Invalid username or password";
      }

      return reason.message;
    }

    return String(reason);
  };

  const switchView = (next: View) => {
    view = next;
    error = "";
    notice = "";
  };

  // page URL used as the magic-link target in mails; the flow query brings
  // the user back to the right form with the code prefilled.
  const pageURL = (flow: string) => {
    return `${window.location.origin}${window.location.pathname}?flow=${flow}`;
  };

  const signup = async (
    e: SubmitEvent & { currentTarget: EventTarget & HTMLFormElement },
  ) => {
    e.preventDefault();
    if (working || !passwordLink?.signup_url) {
      return;
    }

    working = true;
    error = "";
    const data = formToObject(e.currentTarget);
    try {
      const res = await axios.post(passwordLink.signup_url, {
        name: data.name,
        email: data.email,
        password: data.password,
        redirect_uri: pageURL("verify"),
      });

      const payload = res.data?.payload ?? {};
      notice = payload.message ?? "Account request accepted";
      view = payload.verification_required ? "verify" : "signin";
    } catch (reason: unknown) {
      error = axiosError(reason);
    } finally {
      working = false;
    }
  };

  const signupVerify = async (
    e: SubmitEvent & { currentTarget: EventTarget & HTMLFormElement },
  ) => {
    e.preventDefault();
    if (working || !passwordLink?.signup_verify_url) {
      return;
    }

    working = true;
    error = "";
    const data = formToObject(e.currentTarget);
    try {
      const res = await axios.post(passwordLink.signup_verify_url, {
        code: data.code,
      });

      notice = res.data?.payload?.message ?? "Email verified";
      view = "signin";
    } catch (reason: unknown) {
      error = axiosError(reason);
    } finally {
      working = false;
    }
  };

  const resetRequest = async (
    e: SubmitEvent & { currentTarget: EventTarget & HTMLFormElement },
  ) => {
    e.preventDefault();
    if (working || !passwordLink?.password_reset_url) {
      return;
    }

    working = true;
    error = "";
    const data = formToObject(e.currentTarget);
    try {
      const res = await axios.post(passwordLink.password_reset_url, {
        email: data.email,
        redirect_uri: pageURL("reset"),
      });

      notice = res.data?.payload?.message ?? "Check your email";
      view = "reset-confirm";
    } catch (reason: unknown) {
      error = axiosError(reason);
    } finally {
      working = false;
    }
  };

  const resetConfirm = async (
    e: SubmitEvent & { currentTarget: EventTarget & HTMLFormElement },
  ) => {
    e.preventDefault();
    if (working || !passwordLink?.password_reset_confirm_url) {
      return;
    }

    working = true;
    error = "";
    const data = formToObject(e.currentTarget);
    try {
      const res = await axios.post(passwordLink.password_reset_confirm_url, {
        code: data.code,
        password: data.password,
      });

      notice = res.data?.payload?.message ?? "Password updated";
      view = "signin";
    } catch (reason: unknown) {
      error = axiosError(reason);
    } finally {
      working = false;
    }
  };

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
      error = axiosError(reason);
    } finally {
      working = false;
    }
  };

  const passkeySignin = async (url: string) => {
    if (working) {
      return;
    }

    working = true;
    error = "";
    try {
      // username scopes credentials when filled; empty uses discoverable flow
      const usernameInput = document.getElementById("username") as HTMLInputElement | null;
      const username = usernameInput?.value ?? "";

      const begin = await axios.post<{
        session_id: string;
        options: ServerRequestOptions;
      }>(url, username ? { username } : {});

      const assertion = await startAuthentication(begin.data.options);
      if (!assertion) {
        throw new Error("Passkey ceremony was cancelled");
      }

      await axios.post(url, {
        session_id: begin.data.session_id,
        assertion,
      });

      if (!isResponseTypeCode()) {
        window.location.assign(getRedirectPath());

        return;
      }

      window.location.replace(window.location.href);

      return;
    } catch (reason: unknown) {
      if (reason instanceof Error && reason.name === "NotAllowedError") {
        error = "Passkey was cancelled or timed out";
      } else {
        error = axiosError(reason);
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

    // magic links from signup/reset mails come back with flow + code
    const params = new URLSearchParams(window.location.search);
    const flow = params.get("flow");
    const flowCode = params.get("code") ?? "";
    if (flow === "verify") {
      view = "verify";
      verifyCode = flowCode;
    } else if (flow === "reset") {
      view = "reset-confirm";
      resetCode = flowCode;
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
      {#if authInfo.provider.password?.length && view === "signin"}
        {#if authInfo.provider.password?.length > 1}
          <div class="float-right">
            <select bind:value={providerSelected} class="border rounded-md border-gray-300 px-2 py-1 text-sm focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50">
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
              class={inputClass}
            />
          </div>
          <div class="mb-4">
            <label class="block mb-1" for="password">Password</label>
            <PasswordInput id="password" name="password" autocomplete="current-password" />
          </div>
          <div class="mt-6">
            <button type="submit" class={submitClass} disabled={working}>
              Sign in
            </button>
          </div>
        </form>
        {#if canSignup || canReset}
          <div class="mt-4 space-y-3">
            {#if canSignup}
              <button type="button" class={secondaryClass} on:click={() => switchView("signup")}>
                Create account
              </button>
            {/if}
            {#if canReset}
              <div class="text-center">
                <button type="button" class={linkClass} on:click={() => switchView("reset")}>
                  Forgot password?
                </button>
              </div>
            {/if}
          </div>
        {/if}
      {/if}
      {#if view === "signup"}
        <form on:submit|preventDefault|stopPropagation={signup}>
          <div class="mb-4">
            <label class="block mb-1" for="signup-name">Name</label>
            <input id="signup-name" type="text" name="name" class={inputClass} />
          </div>
          <div class="mb-4">
            <label class="block mb-1" for="signup-email">Email</label>
            <input id="signup-email" type="email" name="email" required class={inputClass} />
          </div>
          <div class="mb-4">
            <label class="block mb-1" for="signup-password">Password (min {passwordMinLength} characters)</label>
            <PasswordInput
              id="signup-password"
              name="password"
              minlength={passwordMinLength}
              required
              autocomplete="new-password"
            />
          </div>
          <div class="mt-6">
            <button type="submit" class={submitClass} disabled={working}>Create account</button>
          </div>
        </form>
        <div class="mt-3 flex justify-between">
          <button type="button" class={linkClass} on:click={() => switchView("signin")}>
            Back to sign in
          </button>
          <button type="button" class={linkClass} on:click={() => switchView("verify")}>
            Have a code?
          </button>
        </div>
      {/if}
      {#if view === "verify"}
        <form on:submit|preventDefault|stopPropagation={signupVerify}>
          <div class="mb-4">
            <label class="block mb-1" for="verify-code">Verification code</label>
            <input
              id="verify-code"
              type="text"
              name="code"
              required
              bind:value={verifyCode}
              autocomplete="one-time-code"
              class={inputClass}
            />
          </div>
          <div class="mt-6">
            <button type="submit" class={submitClass} disabled={working}>Verify email</button>
          </div>
        </form>
        <div class="mt-3">
          <button type="button" class={linkClass} on:click={() => switchView("signin")}>
            Back to sign in
          </button>
        </div>
      {/if}
      {#if view === "reset"}
        <form on:submit|preventDefault|stopPropagation={resetRequest}>
          <div class="mb-4">
            <label class="block mb-1" for="reset-email">Email</label>
            <input id="reset-email" type="email" name="email" required class={inputClass} />
          </div>
          <div class="mt-6">
            <button type="submit" class={submitClass} disabled={working}>Send reset email</button>
          </div>
        </form>
        <div class="mt-3 flex justify-between">
          <button type="button" class={linkClass} on:click={() => switchView("signin")}>
            Back to sign in
          </button>
          <button type="button" class={linkClass} on:click={() => switchView("reset-confirm")}>
            Have a code?
          </button>
        </div>
      {/if}
      {#if view === "reset-confirm"}
        <form on:submit|preventDefault|stopPropagation={resetConfirm}>
          <div class="mb-4">
            <label class="block mb-1" for="reset-code">Reset code</label>
            <input
              id="reset-code"
              type="text"
              name="code"
              required
              bind:value={resetCode}
              autocomplete="one-time-code"
              class={inputClass}
            />
          </div>
          <div class="mb-4">
            <label class="block mb-1" for="reset-password">New password (min {passwordMinLength} characters)</label>
            <PasswordInput
              id="reset-password"
              name="password"
              minlength={passwordMinLength}
              required
              autocomplete="new-password"
            />
          </div>
          <div class="mt-6">
            <button type="submit" class={submitClass} disabled={working}>Set new password</button>
          </div>
        </form>
        <div class="mt-3">
          <button type="button" class={linkClass} on:click={() => switchView("signin")}>
            Back to sign in
          </button>
        </div>
      {/if}
      {#if view === "signin" && authInfo.provider.passkey?.length && isWebAuthnSupported()}
        {#if authInfo.provider.password?.length}
          <hr class="mt-8 mb-6 custom-hr" />
        {/if}
        {#each authInfo.provider.passkey as provider}
          <button
            title={provider.url}
            on:click={async () => {
              await passkeySignin(provider.url);
            }}
            disabled={working}
            class={`${secondaryClass} mt-1`}
          >
            <svg
              class="inline-block -mt-0.5 mr-1"
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M2 18v3c0 .6.4 1 1 1h4v-3h3v-3h2l1.4-1.4a6.5 6.5 0 1 0-4-4Z"></path>
              <circle cx="16.5" cy="7.5" r=".5"></circle>
            </svg>
            Sign in with a passkey{authInfo.provider.passkey.length > 1 ? ` (${provider.name})` : ""}
          </button>
        {/each}
      {/if}
      {#if view === "signin" && authInfo.provider.code?.length}
        {#if authInfo.provider.password?.length || authInfo.provider.passkey?.length}
          <hr class="mt-8 mb-6 custom-hr" />
        {/if}
        {#each authInfo.provider.code as provider}
          <button
            title={provider.url}
            on:click={async () => {
              checkWindow(provider.url);
            }}
            class={`${secondaryClass} mt-1 capitalize`}
          >
            {provider.name}
          </button>
        {/each}
      {/if}
      {#if notice != ""}
        <div class="mt-4 rounded-md border border-green-300 bg-green-50 px-3 py-2 text-sm text-green-800">
          <span class="break-all">{notice}</span>
        </div>
      {/if}
      {#if error != ""}
        <div class="mt-4 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800">
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
