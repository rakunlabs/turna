<script lang="ts">
  import { formToObject } from "./helper/form";
  import { onMount, tick } from "svelte";
  import {
    createLogin,
    LoginError,
    isWebAuthnSupported,
    flowFromURL,
    type LoginMethods,
    type LoginLink,
    type PasskeyEnrollmentStatus,
  } from "./sdk";
  import PasswordInput from "./components/PasswordInput.svelte";
  import RememberMe from "./components/RememberMe.svelte";

  // The embedded UI is the reference consumer of the login SDK; external
  // login pages load the same module from `{base}/auth/sdk.js`.
  const loginClient = createLogin({
    base: import.meta.env.DEV ? "/login/" : new URL("./", window.location.href),
  });

  let error = "";
  let notice = "";
  let working = false;
  let pendingCode: number | null = null;
  let loaded = false;
  let loadError = "";
  let rememberMe = false;

  // theme: user choice persisted in localStorage; "system" follows the OS.
  type Theme = "light" | "dark" | "system";
  const themeKey = "login-theme";
  let theme: Theme = "system";
  const themeLabel: Record<Theme, string> = { light: "Light", dark: "Dark", system: "System" };

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

  type View = "signin" | "signup" | "verify" | "reset" | "reset-confirm" | "passkey-enrollment";
  let view: View = "signin";
  let username = "";
  let verifyCode = "";
  let resetCode = "";
  let passkeyLabel = "";
  // address the last signup/reset mail went to, echoed on the code screens
  let sentTo = "";
  let enrollmentStatus: PasskeyEnrollmentStatus | null = null;

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
  $: hasPassword = !!authInfo.provider.password?.length;
  $: passkeys = isWebAuthnSupported() ? authInfo.provider.passkey ?? [] : [];
  $: codes = authInfo.provider.code ?? [];
  $: hasAnyMethod = hasPassword || passkeys.length > 0 || codes.length > 0;
  $: showRememberMe = !authInfo.disable_remember_me && hasAnyMethod;

  $: heading = {
    signin: authInfo.title,
    signup: "Create an account",
    verify: "Verify your email",
    reset: "Reset your password",
    "reset-confirm": "Choose a new password",
    "passkey-enrollment": "Make your next sign-in faster",
  }[view];

  $: subtitle = {
    signin: "",
    signup: "Sign up with your email address.",
    verify: sentTo
      ? `We sent a code to ${sentTo}. Enter it below, or open the link in that email.`
      : "Enter the code from your verification email, or open the link it contains.",
    reset: "Enter your account email and we'll send you a reset code.",
    "reset-confirm": sentTo
      ? `If ${sentTo} belongs to an account, a reset code is on its way. Enter it with your new password.`
      : "Enter the code from your reset email and choose a new password.",
    "passkey-enrollment":
      "Create a passkey for this device and sign in next time with your fingerprint, face, or device PIN.",
  }[view];

  // The SDK normalizes backend {message, error} envelopes, embedded OAuth2
  // error bodies and credential mismatches into LoginError.
  const errorMessage = (reason: unknown) =>
    reason instanceof LoginError ? reason.message : String(reason);

  // Moves focus to the first field of each view so keyboard users and
  // screen readers land on the new step.
  const focusOnMount = (node: HTMLElement) => {
    tick().then(() => node.focus());
  };

  const switchView = (next: View) => {
    view = next;
    error = "";
    notice = "";
  };

  const finishLogin = async () => {
    try {
      const status = await loginClient.passkeyEnrollmentStatus();
      if (status.prompt) {
        enrollmentStatus = status;
        switchView("passkey-enrollment");
        return;
      }
    } catch (reason: unknown) {
      // Enrollment is optional; a policy check must never block a valid login.
      console.error(errorMessage(reason));
    }

    loginClient.finish();
  };

  const enrollPasskey = async () => {
    if (working) return;

    working = true;
    error = "";
    try {
      await loginClient.enrollPasskey({ name: passkeyLabel });
      loginClient.finish();
    } catch (reason: unknown) {
      error = errorMessage(reason);
    } finally {
      working = false;
    }
  };

  const skipPasskeyEnrollment = () => {
    if (enrollmentStatus) loginClient.snoozePasskeyEnrollment(enrollmentStatus);
    loginClient.finish();
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

      if (result.verificationRequired) {
        sentTo = data.email;
        switchView("verify");
      } else {
        switchView("signin");
        notice = result.message;
      }
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
      const message = await loginClient.signupVerify(passwordLink, data.code.trim());
      switchView("signin");
      notice = message;
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
      await loginClient.resetRequest(passwordLink, { email: data.email });
      sentTo = data.email;
      switchView("reset-confirm");
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
      const message = await loginClient.resetConfirm(passwordLink, data.code.trim(), data.password);
      switchView("signin");
      notice = message;
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
    error = "";
    notice = "";
    const data = formToObject(e.currentTarget);
    try {
      if (!passwordLink) {
        throw new LoginError("Provider not found");
      }

      await loginClient.password(passwordLink, {
        username: data.username,
        password: data.password,
        rememberMe,
        extra: data,
      });

      await finishLogin();

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
    notice = "";
    try {
      // username scopes credentials when filled; empty uses discoverable flow
      await loginClient.passkey(link, {
        username: username.trim() || undefined,
        rememberMe,
      });

      await finishLogin();

      return;
    } catch (reason: unknown) {
      error = errorMessage(reason);
    } finally {
      working = false;
    }
  };

  const info = async () => {
    loadError = "";
    try {
      const methods = await loginClient.methods();
      if (methods.provider.password?.length) {
        providerSelected = methods.provider.password[0].name;
      }

      authInfo = methods;
    } catch (reason: unknown) {
      console.error(errorMessage(reason));
      loadError = "Sign-in options could not be loaded. Check your connection and try again.";
    }
  };

  const retryInfo = async () => {
    loaded = false;
    await info();
    loaded = true;
  };

  const codeSignin = (link: LoginLink, index: number) => {
    if (pendingCode === index) return;

    error = "";
    notice = "";
    pendingCode = index;
    loginClient
      .code(link, {
        rememberMe,
        onPopupClosed: () => {
          if (pendingCode === index) pendingCode = null;
          error = `The ${link.name} window closed before sign-in finished. Try again to reopen it.`;
        },
      })
      .then(() => finishLogin())
      .catch((reason: unknown) => {
        error = errorMessage(reason);
      })
      .finally(() => {
        if (pendingCode === index) pendingCode = null;
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

    loaded = true;
  });
</script>

<div class="login-bg relative flex min-h-screen w-full flex-col items-center bg-canvas sm:pt-16">
  <main class="mx-auto w-full pt-8 sm:max-w-[23rem] sm:p-5 sm:pt-0">
    <div class="login-card relative border-y border-divider bg-surface px-5 py-5 text-ink shadow-[0_1px_2px_rgb(0_0_0/0.04),0_8px_24px_-12px_rgb(0_0_0/0.12)] sm:rounded-lg sm:border sm:px-6 sm:py-6">
      <button
        type="button"
        on:click={cycleTheme}
        aria-label={`Theme: ${themeLabel[theme]}. Switch theme`}
        title={`Theme: ${themeLabel[theme]}`}
        class="absolute right-3 top-4 flex h-8 w-8 sm:right-4 sm:top-5 items-center justify-center rounded-md text-muted transition hover:bg-hover hover:text-ink focus:outline-none focus-visible:ring-2 focus-visible:ring-brand/40"
      >
        <!-- lucide sun / moon / monitor -->
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
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

      {#if view === "passkey-enrollment"}
        <div class="mb-5 flex h-12 w-12 items-center justify-center rounded-full bg-brand/10 text-brand">
          <svg
            width="25"
            height="25"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4"></path>
            <path d="M14 13.12c0 2.38 0 6.38-1 8.88"></path>
            <path d="M17.29 21.02c.12-.6.43-2.3.5-3.02"></path>
            <path d="M2 12a10 10 0 0 1 18-6"></path>
            <path d="M2 16h.01"></path>
            <path d="M21.8 16c.2-2 .131-5.354 0-6"></path>
            <path d="M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2"></path>
            <path d="M8.65 22c.21-.66.45-1.32.57-2"></path>
            <path d="M9 6.8a6 6 0 0 1 9 5.2v2"></path>
          </svg>
        </div>
      {/if}

      <h1 class="pr-10 text-xl font-semibold tracking-tight [line-height:1.2] [text-wrap:balance]">
        <span class={loaded ? "" : "invisible"}>{heading}</span>
      </h1>
      {#if subtitle}
        <p class="mt-1 text-sm leading-6 text-muted [overflow-wrap:anywhere]">{subtitle}</p>
      {/if}

      <div class="mt-5">
        {#if notice != ""}
          <div
            role="status"
            class="mb-5 flex gap-2.5 rounded-md border border-success-line bg-success-bg px-3 py-2.5 text-sm text-success"
          >
            <svg class="mt-0.5 shrink-0" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              <circle cx="12" cy="12" r="10"></circle>
              <path d="m9 12 2 2 4-4"></path>
            </svg>
            <span class="min-w-0 [overflow-wrap:anywhere]">{notice}</span>
          </div>
        {/if}
        <div aria-live="assertive" aria-atomic="true">
          {#if error != ""}
            <div
              class="mb-5 flex gap-2.5 rounded-md border border-danger-line bg-danger-bg px-3 py-2.5 text-sm text-danger"
            >
              <svg class="mt-0.5 shrink-0" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                <circle cx="12" cy="12" r="10"></circle>
                <line x1="12" x2="12" y1="8" y2="12"></line>
                <line x1="12" x2="12.01" y1="16" y2="16"></line>
              </svg>
              <span class="min-w-0 [overflow-wrap:anywhere]">{error}</span>
            </div>
          {/if}
        </div>

        {#if !loaded}
          <p role="status" class="py-6 text-center text-sm text-muted">Loading sign-in options…</p>
        {:else if loadError && view === "signin"}
          <div role="alert" class="space-y-3">
            <p class="text-sm leading-6 text-muted">{loadError}</p>
            <button type="button" class="btn btn-secondary" on:click={retryInfo}>Try again</button>
          </div>
        {:else if view === "signin" && !hasAnyMethod}
          <p class="text-sm leading-6 text-muted">
            No sign-in methods are configured for this page. Contact your administrator.
          </p>
        {:else if view === "signin"}
          {#if hasPassword}
            <form on:submit|preventDefault|stopPropagation={signin} class="space-y-3">
              {#if (authInfo.provider.password?.length ?? 0) > 1}
                <div>
                  <label class="block text-sm font-medium" for="provider">Sign in with</label>
                  <select id="provider" bind:value={providerSelected} class="field">
                    {#each authInfo.provider.password ?? [] as provider}
                      <option value={provider.name}>{provider.name}</option>
                    {/each}
                  </select>
                </div>
              {/if}
              <div>
                <label class="block text-sm font-medium" for="username">Username</label>
                <input
                  id="username"
                  type="text"
                  name="username"
                  required
                  bind:value={username}
                  autocomplete="username"
                  autocapitalize="none"
                  spellcheck="false"
                  class="field"
                  use:focusOnMount
                />
              </div>
              <div>
                <div class="flex items-baseline justify-between">
                  <label class="block text-sm font-medium" for="password">Password</label>
                  {#if canReset}
                    <button type="button" class="link" on:click={() => switchView("reset")}>
                      Forgot password?
                    </button>
                  {/if}
                </div>
                <PasswordInput id="password" name="password" autocomplete="current-password" required />
              </div>
              {#if showRememberMe}
                <RememberMe bind:checked={rememberMe} />
              {/if}
              <button type="submit" class="btn btn-primary !mt-5" disabled={working}>
                {working ? "Signing in…" : "Sign in"}
              </button>
            </form>
          {:else if showRememberMe}
            <RememberMe bind:checked={rememberMe} />
          {/if}

          {#if passkeys.length || codes.length}
            {#if hasPassword}
              <div class="my-5 flex items-center gap-3 text-xs text-muted" role="separator">
                <span class="h-px flex-1 bg-divider"></span>
                Or continue with
                <span class="h-px flex-1 bg-divider"></span>
              </div>
            {:else}
              <div class="mt-5"></div>
            {/if}
            <div class="space-y-2">
              {#each passkeys as provider}
                <button
                  type="button"
                  title={provider.url}
                  on:click={() => passkeySignin(provider)}
                  disabled={working}
                  class="btn btn-secondary"
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M2 18v3c0 .6.4 1 1 1h4v-3h3v-3h2l1.4-1.4a6.5 6.5 0 1 0-4-4Z"></path>
                    <circle cx="16.5" cy="7.5" r=".5"></circle>
                  </svg>
                  Passkey{passkeys.length > 1 ? ` (${provider.name})` : ""}
                </button>
              {/each}
              {#each codes as provider, index}
                <button
                  type="button"
                  title={provider.url}
                  on:click={() => codeSignin(provider, index)}
                  disabled={working || pendingCode === index}
                  aria-busy={pendingCode === index}
                  class="btn btn-secondary"
                >
                  <span class="min-w-0 truncate capitalize">
                    {pendingCode === index ? `Waiting for ${provider.name}…` : provider.name}
                  </span>
                </button>
              {/each}
            </div>
          {/if}

          {#if canSignup}
            <p class="mt-5 text-center text-sm text-muted">
              New here?
              <button type="button" class="link" on:click={() => switchView("signup")}>Create an account</button>
            </p>
          {/if}
        {:else if view === "signup"}
          <form on:submit|preventDefault|stopPropagation={signup} class="space-y-3">
            <div>
              <label class="block text-sm font-medium" for="signup-name">
                Name <span class="font-normal text-muted">(optional)</span>
              </label>
              <input id="signup-name" type="text" name="name" autocomplete="name" class="field" use:focusOnMount />
            </div>
            <div>
              <label class="block text-sm font-medium" for="signup-email">Email</label>
              <input id="signup-email" type="email" name="email" required autocomplete="email" class="field" />
            </div>
            <div>
              <label class="block text-sm font-medium" for="signup-password">Password</label>
              <PasswordInput
                id="signup-password"
                name="password"
                minlength={passwordMinLength}
                required
                autocomplete="new-password"
                describedby="signup-password-hint"
              />
              <p id="signup-password-hint" class="mt-1.5 text-xs text-muted">
                At least {passwordMinLength} characters.
              </p>
            </div>
            <button type="submit" class="btn btn-primary !mt-5" disabled={working}>
              {working ? "Creating account…" : "Create account"}
            </button>
          </form>
          <div class="mt-5 flex justify-between gap-4">
            <button type="button" class="link" on:click={() => switchView("signin")}>Back to sign in</button>
            <button type="button" class="link" on:click={() => switchView("verify")}>I have a code</button>
          </div>
        {:else if view === "verify"}
          <form on:submit|preventDefault|stopPropagation={signupVerify} class="space-y-3">
            <div>
              <label class="block text-sm font-medium" for="verify-code">Verification code</label>
              <input
                id="verify-code"
                type="text"
                name="code"
                required
                bind:value={verifyCode}
                autocomplete="one-time-code"
                autocapitalize="none"
                spellcheck="false"
                class="field font-mono tracking-wide"
                use:focusOnMount
              />
            </div>
            <button type="submit" class="btn btn-primary !mt-5" disabled={working}>
              {working ? "Verifying…" : "Verify email"}
            </button>
          </form>
          <div class="mt-5">
            <button type="button" class="link" on:click={() => switchView("signin")}>Back to sign in</button>
          </div>
        {:else if view === "reset"}
          <form on:submit|preventDefault|stopPropagation={resetRequest} class="space-y-3">
            <div>
              <label class="block text-sm font-medium" for="reset-email">Email</label>
              <input id="reset-email" type="email" name="email" required autocomplete="email" class="field" use:focusOnMount />
            </div>
            <button type="submit" class="btn btn-primary !mt-5" disabled={working}>
              {working ? "Sending…" : "Send reset code"}
            </button>
          </form>
          <div class="mt-5 flex justify-between gap-4">
            <button type="button" class="link" on:click={() => switchView("signin")}>Back to sign in</button>
            <button type="button" class="link" on:click={() => switchView("reset-confirm")}>I have a code</button>
          </div>
        {:else if view === "reset-confirm"}
          <form on:submit|preventDefault|stopPropagation={resetConfirm} class="space-y-3">
            <div>
              <label class="block text-sm font-medium" for="reset-code">Reset code</label>
              <input
                id="reset-code"
                type="text"
                name="code"
                required
                bind:value={resetCode}
                autocomplete="one-time-code"
                autocapitalize="none"
                spellcheck="false"
                class="field font-mono tracking-wide"
                use:focusOnMount
              />
            </div>
            <div>
              <label class="block text-sm font-medium" for="reset-password">New password</label>
              <PasswordInput
                id="reset-password"
                name="password"
                minlength={passwordMinLength}
                required
                autocomplete="new-password"
                describedby="reset-password-hint"
              />
              <p id="reset-password-hint" class="mt-1.5 text-xs text-muted">
                At least {passwordMinLength} characters.
              </p>
            </div>
            <button type="submit" class="btn btn-primary !mt-5" disabled={working}>
              {working ? "Saving…" : "Set new password"}
            </button>
          </form>
          <div class="mt-5">
            <button type="button" class="link" on:click={() => switchView("signin")}>Back to sign in</button>
          </div>
        {:else if view === "passkey-enrollment"}
          <div>
            <label class="block text-sm font-medium" for="passkey-label">
              Device name <span class="font-normal text-muted">(optional)</span>
            </label>
            <input
              id="passkey-label"
              type="text"
              bind:value={passkeyLabel}
              autocomplete="off"
              placeholder="Work laptop"
              class="field"
            />
          </div>

          <div class="mt-6 space-y-3">
            <button type="button" class="btn btn-primary" disabled={working} on:click={enrollPasskey} use:focusOnMount>
              {working ? "Waiting for this device…" : "Create passkey"}
            </button>
            <button type="button" class="btn btn-secondary" disabled={working} on:click={skipPasskeyEnrollment}>
              Not now
            </button>
          </div>
          <p class="mt-4 text-xs leading-5 text-muted">
            Your biometric data stays on this device. Turna stores only the public credential.
          </p>
        {/if}
      </div>
    </div>
  </main>
</div>

<style lang="scss">
  // WhatsApp-style faint doodle wallpaper: auth-themed lucide outlines
  // (key, lock, fingerprint, shield, at-sign, user, phone, badge, mail)
  // tiled at low opacity, plus a soft brand-tinted glow at the top.
  @function doodles($stroke, $opacity) {
    @return url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='260' height='260' viewBox='0 0 260 260'%3E%3Cg fill='none' stroke='#{$stroke}' stroke-opacity='#{$opacity}' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Cg transform='translate(18 16) rotate(-12 12 12)'%3E%3Cpath d='M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z'/%3E%3Ccircle cx='16.5' cy='7.5' r='.5'/%3E%3C/g%3E%3Cg transform='translate(112 26) rotate(9 12 12)'%3E%3Crect width='18' height='11' x='3' y='11' rx='2'/%3E%3Cpath d='M7 11V7a5 5 0 0 1 10 0v4'/%3E%3C/g%3E%3Cg transform='translate(198 12) rotate(-7 12 12)'%3E%3Cpath d='M12 10a2 2 0 0 0-2 2c0 1.02-.1 2.51-.26 4'/%3E%3Cpath d='M14 13.12c0 2.38 0 6.38-1 8.88'/%3E%3Cpath d='M17.29 21.02c.12-.6.43-2.3.5-3.02'/%3E%3Cpath d='M2 12a10 10 0 0 1 18-6'/%3E%3Cpath d='M2 16h.01'/%3E%3Cpath d='M21.8 16c.2-2 .131-5.354 0-6'/%3E%3Cpath d='M5 19.5C5.5 18 6 15 6 12a6 6 0 0 1 .34-2'/%3E%3Cpath d='M8.65 22c.21-.66.45-1.32.57-2'/%3E%3Cpath d='M9 6.8a6 6 0 0 1 9 5.2v2'/%3E%3C/g%3E%3Cg transform='translate(58 102) rotate(7 12 12)'%3E%3Cpath d='M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1 1 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'/%3E%3C/g%3E%3Cg transform='translate(148 94) rotate(-11 12 12)'%3E%3Ccircle cx='12' cy='12' r='4'/%3E%3Cpath d='M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8'/%3E%3C/g%3E%3Cg transform='translate(226 108) rotate(10 12 12)'%3E%3Ccircle cx='12' cy='8' r='5'/%3E%3Cpath d='M20 21a8 8 0 0 0-16 0'/%3E%3C/g%3E%3Cg transform='translate(22 194) rotate(8 12 12)'%3E%3Crect width='14' height='20' x='5' y='2' rx='2'/%3E%3Cpath d='M12 18h.01'/%3E%3C/g%3E%3Cg transform='translate(116 206) rotate(-9 12 12)'%3E%3Cpath d='M3.85 8.62a4 4 0 0 1 4.78-4.77 4 4 0 0 1 6.74 0 4 4 0 0 1 4.78 4.78 4 4 0 0 1 0 6.74 4 4 0 0 1-4.77 4.78 4 4 0 0 1-6.75 0 4 4 0 0 1-4.78-4.77 4 4 0 0 1 0-6.76Z'/%3E%3Cpath d='m9 12 2 2 4-4'/%3E%3C/g%3E%3Cg transform='translate(202 198) rotate(6 12 12)'%3E%3Crect width='20' height='16' x='2' y='4' rx='2'/%3E%3Cpath d='m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7'/%3E%3C/g%3E%3C/g%3E%3C/svg%3E");
  }

  .login-bg {
    background-image:
      radial-gradient(48rem 26rem at 50% -8rem, rgb(var(--brand) / 0.07), transparent 70%),
      doodles("%23475569", ".05");
    background-repeat: no-repeat, repeat;
    background-size: auto, 260px 260px;
  }

  :global(html.dark) .login-bg {
    background-image:
      radial-gradient(48rem 26rem at 50% -8rem, rgb(var(--brand) / 0.08), transparent 70%),
      doodles("%23FFFCF2", ".035");
  }

  // Checker texture on the login card's own surface: a tiny conic-gradient
  // checkerboard layered over the card colour, dark specks on light and
  // light specks on dark. Fields and buttons keep their solid fills.
  .login-card {
    --grain: #0000000a;
    background-image: conic-gradient(
      var(--grain) 90deg,
      transparent 90deg 180deg,
      var(--grain) 180deg 270deg,
      transparent 270deg
    );
    background-size: 4px 4px;
  }

  :global(html.dark) .login-card {
    --grain: #ffffff14;
  }
</style>
