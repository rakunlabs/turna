<script lang="ts">
  import { formToObject } from "./helper/form";
  import { onMount } from "svelte";
  import {
    createLogin,
    LoginError,
    isWebAuthnSupported,
    flowFromURL,
    type LoginMethods,
    type LoginLink,
  } from "./sdk";
  import PasswordInput from "./components/PasswordInput.svelte";

  // The embedded UI is the reference consumer of the login SDK; external
  // login pages load the same module from `{base}/auth/sdk.js`.
  const loginClient = createLogin({
    base: import.meta.env.DEV ? "/login/" : new URL("./", window.location.href),
  });

  let error = "";
  let notice = "";
  let working = false;
  let mounted = false;
  let rememberMe = false;

  // theme: user choice persisted in localStorage; "system" follows the OS.
  type Theme = "light" | "dark" | "system";
  const themeKey = "login-theme";
  let theme: Theme = "system";

  const applyTheme = (t: Theme) => {
    const dark =
      t === "dark" ||
      (t === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", dark);
  };

  const setTheme = (t: Theme) => {
    theme = t;
    try {
      localStorage.setItem(themeKey, t);
    } catch {
      // storage unavailable: theme lives for this page only
    }
    applyTheme(t);
  };

  const cycleTheme = () => {
    setTheme(theme === "system" ? "light" : theme === "light" ? "dark" : "system");
  };

  const initTheme = () => {
    try {
      const stored = localStorage.getItem(themeKey);
      if (stored === "light" || stored === "dark" || stored === "system") {
        theme = stored;
      }
    } catch {
      // storage unavailable
    }
    applyTheme(theme);
    window
      .matchMedia("(prefers-color-scheme: dark)")
      .addEventListener("change", () => {
        if (theme === "system") applyTheme("system");
      });
  };

  type View = "signin" | "signup" | "verify" | "reset" | "reset-confirm";
  let view: View = "signin";
  let verifyCode = "";
  let resetCode = "";

  const inputClass =
    "py-1.5 px-3 border rounded-md border-gray-300 bg-white text-gray-900 focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50 disabled:bg-gray-100 mt-1 block w-full dark:border-gray-700 dark:bg-gray-950 dark:text-gray-100 dark:placeholder-gray-500 dark:focus:border-blue-700 dark:focus:ring-blue-800 dark:disabled:bg-gray-800";
  const submitClass =
    "block w-full text-center px-4 py-1.5 bg-[#615fff] border rounded-md border-transparent font-semibold capitalize text-white hover:bg-blue-500 active:bg-blue-500 focus:outline-none focus:border-blue-500 focus:ring focus:ring-blue-200 disabled:bg-gray-400 transition dark:focus:ring-blue-800 dark:disabled:bg-gray-600";
  const secondaryClass =
    "block w-full text-center px-4 py-1.5 bg-white border border-gray-300 rounded-md font-semibold text-black hover:bg-gray-50 active:bg-blue-50 focus:outline-none focus:border-blue-500 focus:ring focus:ring-blue-200 disabled:bg-gray-400 transition dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100 dark:hover:bg-gray-800 dark:active:bg-gray-800 dark:focus:ring-blue-800 dark:disabled:bg-gray-700";
  const linkClass = "text-sm text-blue-600 hover:underline cursor-pointer bg-transparent border-0 p-0 dark:text-blue-400";

  let authInfo: LoginMethods = {
    title: "Login",
    provider: {
      password: [],
      code: [],
    },
  };


  let providerSelected = "";

  $: passwordLink = authInfo.provider.password?.find((v) => v.name == providerSelected);
  $: canSignup = !!passwordLink?.signup_url;
  $: canReset = !!passwordLink?.password_reset_url;
  $: passwordMinLength = passwordLink?.password_min_length || 8;

  // The SDK normalizes backend {message, error} envelopes, embedded OAuth2
  // error bodies and credential mismatches into LoginError.
  const errorMessage = (reason: unknown) =>
    reason instanceof LoginError ? reason.message : String(reason);

  const switchView = (next: View) => {
    view = next;
    error = "";
    notice = "";
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
      const result = await loginClient.signup(passwordLink, {
        name: data.name,
        email: data.email,
        password: data.password,
      });

      notice = result.message;
      view = result.verificationRequired ? "verify" : "signin";
    } catch (reason: unknown) {
      error = errorMessage(reason);
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
      notice = await loginClient.signupVerify(passwordLink, data.code);
      view = "signin";
    } catch (reason: unknown) {
      error = errorMessage(reason);
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
      notice = await loginClient.resetRequest(passwordLink, { email: data.email });
      view = "reset-confirm";
    } catch (reason: unknown) {
      error = errorMessage(reason);
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
      notice = await loginClient.resetConfirm(passwordLink, data.code, data.password);
      view = "signin";
    } catch (reason: unknown) {
      error = errorMessage(reason);
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
      const link = authInfo.provider.password?.find((v) => v.name == providerSelected);
      if (!link) {
        throw new LoginError("Provider not found");
      }

      await loginClient.password(link, {
        username: data.username,
        password: data.password,
        rememberMe,
        extra: data,
      });

      loginClient.finish();

      return;
    } catch (reason: unknown) {
      error = errorMessage(reason);
    } finally {
      working = false;
    }
  };

  const passkeySignin = async (link: LoginLink) => {
    if (working) {
      return;
    }

    working = true;
    error = "";
    try {
      // username scopes credentials when filled; empty uses discoverable flow
      const usernameInput = document.getElementById("username") as HTMLInputElement | null;
      const username = usernameInput?.value ?? "";

      await loginClient.passkey(link, {
        username: username || undefined,
        rememberMe,
      });

      loginClient.finish();

      return;
    } catch (reason: unknown) {
      error = errorMessage(reason);
    } finally {
      working = false;
    }
  };

  const info = async () => {
    try {
      const methods = await loginClient.methods();
      if (methods.provider.password?.length) {
        providerSelected = methods.provider.password[0].name;
      }

      authInfo = methods;
    } catch (reason: unknown) {
      console.error(errorMessage(reason));
    }
  };

  const codeSignin = (link: LoginLink) => {
    error = "";
    loginClient
      .code(link, {
        rememberMe,
        onPopupClosed: () => {
          error = "The sign-in window may have closed before authentication completed.";
        },
      })
      .then(() => loginClient.finish())
      .catch((reason: unknown) => {
        error = errorMessage(reason);
      });
  };

  onMount(async () => {
    initTheme();
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
    const flowState = flowFromURL();
    if (flowState?.flow === "verify") {
      view = "verify";
      verifyCode = flowState.code;
    } else if (flowState?.flow === "reset") {
      view = "reset-confirm";
      resetCode = flowState.code;
    }

    mounted = true;
  });
</script>

<div class="login-bg w-full min-h-screen bg-gray-50 flex flex-col items-center sm:pt-6 dark:bg-gray-950">
  <div class="w-full sm:max-w-md sm:p-5 mx-auto">
    <div class="border border-gray-200 p-4 bg-white text-gray-900 relative shadow-sm sm:rounded-md dark:border-gray-800 dark:bg-gray-900 dark:text-gray-100">
      <h2 class="mb-2 pr-10 text-xl font-bold [line-height:1.2]">
        <span class={mounted ? "" : "invisible"}>{authInfo.title}</span>
      </h2>
      <hr class="mb-2 border-gray-200 dark:border-gray-800" />
      {#if authInfo.provider.password?.length && view === "signin"}
        {#if authInfo.provider.password?.length > 1}
          <div class="float-right">
            <select bind:value={providerSelected} class="border rounded-md border-gray-300 bg-white px-2 py-1 text-sm focus:border-blue-300 focus:outline-none focus:ring focus:ring-blue-200 focus:ring-opacity-50 dark:border-gray-700 dark:bg-gray-950 dark:text-gray-100 dark:focus:border-blue-700 dark:focus:ring-blue-800">
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
          <hr class="mt-8 mb-6 custom-hr border-gray-200 text-gray-600 dark:border-gray-800 dark:text-gray-400" />
        {/if}
        {#each authInfo.provider.passkey as provider}
          <button
            title={provider.url}
            on:click={async () => {
              await passkeySignin(provider);
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
          <hr class="mt-8 mb-6 custom-hr border-gray-200 text-gray-600 dark:border-gray-800 dark:text-gray-400" />
        {/if}
        {#each authInfo.provider.code as provider}
          <button
            title={provider.url}
            on:click={() => {
              codeSignin(provider);
            }}
            class={`${secondaryClass} mt-1 capitalize`}
          >
            {provider.name}
          </button>
        {/each}
      {/if}
      {#if view === "signin" && !authInfo.disable_remember_me && (authInfo.provider.password?.length || authInfo.provider.passkey?.length || authInfo.provider.code?.length)}
        <label
          title="Keep this sign-in active while you use the site, subject to the server's maximum session lifetime."
          class="mt-6 flex cursor-pointer items-center gap-3 rounded-md border border-gray-200 bg-gray-50 px-3 py-2.5 dark:border-gray-800 dark:bg-gray-950"
        >
          <input
            type="checkbox"
            bind:checked={rememberMe}
            class="h-4 w-4 rounded border-gray-300 text-[#615fff] focus:ring-2 focus:ring-blue-200 dark:border-gray-600 dark:bg-gray-900 dark:focus:ring-blue-800"
          />
          <span class="min-w-0 text-sm font-semibold text-gray-900 dark:text-gray-100">Remember me</span>
        </label>
      {/if}
      {#if notice != ""}
        <div class="mt-4 rounded-md border border-green-300 bg-green-50 px-3 py-2 text-sm text-green-800 dark:border-green-800 dark:bg-green-950 dark:text-green-300">
          <span class="break-all">{notice}</span>
        </div>
      {/if}
      {#if error != ""}
        <div class="mt-4 rounded-md border border-red-300 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-800 dark:bg-red-950 dark:text-red-300">
          <span class="break-all">{error}</span>
        </div>
      {/if}
      <button
        type="button"
        on:click={cycleTheme}
        aria-label={`Theme: ${theme} — click to switch`}
        title={`Theme: ${theme}`}
        class="absolute right-2.5 top-2.5 flex h-8 w-8 items-center justify-center rounded-md text-gray-500 transition hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus:ring focus:ring-blue-200 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-200 dark:focus:ring-blue-800"
      >
        <!-- lucide sun / moon / monitor -->
        <svg
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          {#if theme === "light"}
            <circle cx="12" cy="12" r="4"></circle>
            <path d="M12 2v2"></path>
            <path d="M12 20v2"></path>
            <path d="m4.93 4.93 1.41 1.41"></path>
            <path d="m17.66 17.66 1.41 1.41"></path>
            <path d="M2 12h2"></path>
            <path d="M20 12h2"></path>
            <path d="m6.34 17.66-1.41 1.41"></path>
            <path d="m19.07 4.93-1.41 1.41"></path>
          {:else if theme === "dark"}
            <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"></path>
          {:else}
            <rect width="20" height="14" x="2" y="3" rx="2"></rect>
            <line x1="8" x2="16" y1="21" y2="21"></line>
            <line x1="12" x2="12" y1="17" y2="21"></line>
          {/if}
        </svg>
      </button>
    </div>
  </div>
</div>

<style lang="scss">
  .custom-hr {
    overflow: visible;
    text-align: center;

    &::after {
      position: relative;
      content: "Or continue with";
      top: -13px;
      padding-inline: 0.5rem;
      background-color: #fff;
    }
  }

  :global(html.dark) .custom-hr::after {
    background-color: #111827; // gray-900, matches the card
  }

  // WhatsApp-style faint doodle wallpaper: auth-themed lucide outlines
  // (key, lock, fingerprint, shield, at-sign, user, phone, badge, mail)
  // tiled at low opacity, plus a soft brand-tinted glow at the top.
  @function doodles($stroke, $opacity) {
    @return url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='260' height='260' viewBox='0 0 260 260'%3E%3Cg fill='none' stroke='#{$stroke}' stroke-opacity='#{$opacity}' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cg transform='translate(18 16) rotate(-12 12 12)'%3E%3Cpath d='M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z'/%3E%3Ccircle cx='16.5' cy='7.5' r='.5'/%3E%3C/g%3E%3Cg transform='translate(112 26) rotate(9 12 12)'%3E%3Crect width='18' height='11' x='3' y='11' rx='2'/%3E%3Cpath d='M7 11V7a5 5 0 0 1 10 0v4'/%3E%3C/g%3E%3Cg transform='translate(198 12) rotate(-7 12 12)'%3E%3Cpath d='M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4'/%3E%3Cpath d='M14 13.12c0 2.38 0 6.38-1 8.88'/%3E%3Cpath d='M17.29 21.02c.12-.6.43-2.3.5-3.02'/%3E%3Cpath d='M2 12a10 10 0 0 1 18-6'/%3E%3Cpath d='M2 16h.01'/%3E%3Cpath d='M21.8 16c.2-2 .131-5.354 0-6'/%3E%3Cpath d='M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2'/%3E%3Cpath d='M8.65 22c.21-.66.45-1.32.57-2'/%3E%3Cpath d='M9 6.8a6 6 0 0 1 9 5.2v2'/%3E%3C/g%3E%3Cg transform='translate(58 102) rotate(7 12 12)'%3E%3Cpath d='M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1 1 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'/%3E%3C/g%3E%3Cg transform='translate(148 94) rotate(-11 12 12)'%3E%3Ccircle cx='12' cy='12' r='4'/%3E%3Cpath d='M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8'/%3E%3C/g%3E%3Cg transform='translate(226 108) rotate(10 12 12)'%3E%3Ccircle cx='12' cy='8' r='5'/%3E%3Cpath d='M20 21a8 8 0 0 0-16 0'/%3E%3C/g%3E%3Cg transform='translate(22 194) rotate(8 12 12)'%3E%3Crect width='14' height='20' x='5' y='2' rx='2'/%3E%3Cpath d='M12 18h.01'/%3E%3C/g%3E%3Cg transform='translate(116 206) rotate(-9 12 12)'%3E%3Cpath d='M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 1-4.78-4.77 4 4 0 0 1 0-6.76Z'/%3E%3Cpath d='m9 12 2 2 4-4'/%3E%3C/g%3E%3Cg transform='translate(202 198) rotate(6 12 12)'%3E%3Crect width='20' height='16' x='2' y='4' rx='2'/%3E%3Cpath d='m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7'/%3E%3C/g%3E%3C/g%3E%3C/svg%3E");
  }

  .login-bg {
    background-image:
      radial-gradient(48rem 26rem at 50% -8rem, rgb(97 95 255 / 0.07), transparent 70%),
      doodles("%23475569", ".09");
    background-repeat: no-repeat, repeat;
    background-size: auto, 260px 260px;
  }

  :global(html.dark) .login-bg {
    background-image:
      radial-gradient(48rem 26rem at 50% -8rem, rgb(97 95 255 / 0.13), transparent 70%),
      doodles("%2394a3b8", ".055");
  }
</style>
