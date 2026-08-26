/**
 * Turna login SDK.
 *
 * Framework-agnostic, zero-dependency browser client for the login
 * middleware's flows: method discovery, password, passkey (WebAuthn),
 * authorization-code popup, signup, email verification and password reset.
 *
 * The embedded login UI is built on this module, so external login pages
 * that use it get the exact same behavior. It is published unbundled at a
 * stable URL under the login base path, e.g. `/login/auth/sdk.js`.
 *
 * The SDK assumes it runs on the same origin as the login middleware:
 * every request is sent with same-origin credentials and the session
 * cookie stays owned by the session middleware.
 */

import {
  isWebAuthnSupported,
  startAuthentication,
  type ServerRequestOptions,
} from "../helper/webauthn";

export { isWebAuthnSupported };

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** One login choice from the methods manifest. */
export type LoginLink = {
  name: string;
  url: string;
  signup_url?: string;
  signup_verify_url?: string;
  password_reset_url?: string;
  password_reset_confirm_url?: string;
  password_min_length?: number;
};

export type LoginProvider = {
  password: LoginLink[] | null;
  code: LoginLink[] | null;
  passkey?: LoginLink[] | null;
};

/** Payload of `GET {base}auth/methods`. */
export type LoginMethods = {
  title: string;
  disable_remember_me?: boolean;
  provider: LoginProvider;
  error?: string;
};

export type RememberOption = {
  /**
   * Ask the issuer for a sliding ("remember me") session. Honor
   * `LoginMethods.disable_remember_me`: when it is true the server forces
   * this off regardless of what is sent.
   */
  rememberMe?: boolean;
};

export type PasswordOptions = RememberOption & {
  username: string;
  password: string;
  /** Extra JSON fields merged into the token request body. */
  extra?: Record<string, unknown>;
};

export type PasskeyOptions = RememberOption & {
  /**
   * Optional account hint. Empty or omitted starts the discoverable
   * (username-less) flow: the browser lists resident passkeys and the
   * user picks an account.
   */
  username?: string;
};

export type CodeOptions = RememberOption & {
  /** window.open target, defaults to "_blank". */
  target?: string;
  /**
   * window.open features string, e.g. "width=520,height=720". Defaults to
   * a centered popup window: browsers refuse to focus background tabs, so
   * only a real popup window reliably hands focus back to this page when
   * the sign-in finishes. Pass an empty string to open a tab instead.
   */
  features?: string;
  /** Abort waiting for the popup result. */
  signal?: AbortSignal;
  /**
   * Called once when the popup looks closed while the sign-in has not
   * completed. Only a hint for showing a message: COOP-enabled providers
   * sever the popup handle and report closed while the window is still
   * open, so the flow keeps waiting and a late login still completes.
   */
  onPopupClosed?: () => void;
};

export type SignupOptions = {
  email: string;
  password: string;
  name?: string;
  /**
   * Magic-link target put into the verification mail. Defaults to the
   * current page with `?flow=verify` appended.
   */
  redirectUri?: string;
};

export type SignupResult = {
  message: string;
  /** True when the account still needs an emailed verification code. */
  verificationRequired: boolean;
};

export type ResetRequestOptions = {
  email: string;
  /** Defaults to the current page with `?flow=reset` appended. */
  redirectUri?: string;
};

/** Magic-link state carried back to the login page by mail links. */
export type FlowState = {
  flow: "verify" | "reset";
  code: string;
} | null;

/** Normalized error for every SDK operation. */
export class LoginError extends Error {
  /** HTTP status when the failure came from a response. */
  status?: number;
  /** True for wrong username/password/secret style failures. */
  credentials: boolean;

  constructor(message: string, options?: { status?: number; credentials?: boolean }) {
    super(message);
    this.name = "LoginError";
    this.status = options?.status;
    this.credentials = options?.credentials ?? false;
  }
}

export type CreateLoginOptions = {
  /**
   * Login middleware base path, e.g. "/login/". When the SDK is loaded
   * from its served URL (`{base}auth/sdk.js`) the base is derived
   * automatically; set it explicitly when bundling the SDK source into
   * your own build.
   */
  base?: string | URL;
};

// ---------------------------------------------------------------------------
// Internals
// ---------------------------------------------------------------------------

// Credential-mismatch details are collapsed into one neutral message so a
// login page never reveals whether the user or the password was wrong.
const credentialErrors = ["password not match", "user not found", "secret not match"];
const credentialMessage = "Invalid username or password";
const loginSuccessMessage = "turna:login:success";
const popupFlowParam = "turna_popup";
// Correlation id for one popup sign-in. The server echoes it back as a
// cookie-name suffix so nested login windows never observe each other's
// completion. Keep in sync with the login middleware.
const flowIDParam = "turna_flow";
const successCookie = "auth_verify";

/** Read the standard {message, error} envelope, unwrapping any embedded
 * OAuth2 error body, and map credential failures to a friendly message. */
const responseError = async (response: Response): Promise<LoginError> => {
  let detail: string | undefined;

  try {
    const data = await response.json();
    if (data && typeof data === "object") {
      detail = data.message ?? data.error_description ?? data.error;
    } else if (typeof data === "string") {
      detail = data;
    }
  } catch {
    // body was empty or not JSON
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
      return new LoginError(credentialMessage, { status: response.status, credentials: true });
    }

    return new LoginError(detail, { status: response.status });
  }

  if (response.status === 401) {
    return new LoginError(credentialMessage, { status: 401, credentials: true });
  }

  return new LoginError(`Request failed with status ${response.status}`, {
    status: response.status,
  });
};

const asLoginError = (reason: unknown): LoginError => {
  if (reason instanceof LoginError) return reason;
  if (reason instanceof Error) {
    if (reason.name === "NotAllowedError") {
      return new LoginError("Passkey was cancelled or timed out");
    }

    return new LoginError(reason.message);
  }

  return new LoginError(String(reason));
};

const request = async (url: string | URL, init?: RequestInit): Promise<Response> => {
  let response: Response;
  try {
    response = await fetch(url, { credentials: "same-origin", ...init });
  } catch (reason) {
    throw asLoginError(reason);
  }

  if (!response.ok) {
    throw await responseError(response);
  }

  return response;
};

const postJSON = async (url: string | URL, body: unknown): Promise<any> => {
  const response = await request(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  if (response.status === 204) return undefined;

  return response.json().catch(() => undefined);
};

const readCookie = (name: string): string | undefined => {
  for (const part of document.cookie.split("; ")) {
    const eq = part.indexOf("=");
    if (eq > 0 && part.slice(0, eq) === name) {
      return decodeURIComponent(part.slice(eq + 1));
    }
  }

  return undefined;
};

/** Best-effort cleanup of a consumed marker cookie; it expires on its own. */
const dropCookie = (name: string): void => {
  document.cookie = `${name}=; Max-Age=0; path=/; SameSite=Lax`;
};

/**
 * A real popup window centered on the opener. A plain "_blank" open creates
 * a tab, and closed tabs do not reliably re-activate their opener (Firefox
 * drops the owner link once focus moved through other tabs, and scripted
 * focus() on a background tab is ignored by every browser). Closing the
 * last popup *window* always reveals the opener's window.
 */
const defaultPopupFeatures = (): string => {
  const width = 520;
  const height = 720;
  const outerWidth = window.outerWidth || screen.width;
  const outerHeight = window.outerHeight || screen.height;
  const baseX = window.screenX ?? 0;
  const baseY = window.screenY ?? 0;
  const left = Math.max(0, Math.round(baseX + (outerWidth - width) / 2));
  const top = Math.max(0, Math.round(baseY + (outerHeight - height) / 2));

  return `popup=yes,width=${width},height=${height},left=${left},top=${top}`;
};

/**
 * Per-popup id, restricted to the character set the middleware accepts as a
 * cookie-name suffix ([A-Za-z0-9_-]).
 */
const newFlowID = (): string => {
  const bytes = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues) {
    globalThis.crypto.getRandomValues(bytes);
  } else {
    for (let i = 0; i < bytes.length; i += 1) bytes[i] = Math.floor(Math.random() * 256);
  }

  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
};

/** Current page URL with a `flow` marker, used as mail magic-link target. */
const pageURL = (flow: string): string =>
  `${window.location.origin}${window.location.pathname}?flow=${flow}`;

// ---------------------------------------------------------------------------
// Standalone helpers (also used by finish())
// ---------------------------------------------------------------------------

/** True when the login page runs inside Turna's own authorization-code
 * flow and must reload instead of redirecting after a sign-in. */
export const isResponseTypeCode = (): boolean =>
  new URLSearchParams(window.location.search).get("response_type") === "code";

/** Validated `redirect_path`, or undefined when there is nothing safe to
 * follow. Rejects cross-origin targets and the login page itself. */
const safeRedirectPath = (): string | undefined => {
  const redirectPath = new URLSearchParams(window.location.search).get("redirect_path");

  if (redirectPath?.startsWith("/") && !redirectPath.startsWith("//")) {
    try {
      const target = new URL(redirectPath, window.location.origin);
      const targetPath = target.pathname.replace(/\/+$/, "") || "/";
      const loginPath = window.location.pathname.replace(/\/+$/, "") || "/";
      if (target.origin === window.location.origin && targetPath !== loginPath) {
        return `${target.pathname}${target.search}${target.hash}`;
      }
    } catch {
      // fall through to the safe same-origin default
    }
  }

  return undefined;
};

/** Safe same-origin redirect target from the `redirect_path` query
 * parameter; falls back to "/" and never redirects back to the login page. */
export const getRedirectPath = (): string => safeRedirectPath() ?? "/";

/** Magic-link state (`?flow=verify|reset&code=...`) from the current URL. */
export const flowFromURL = (): FlowState => {
  const params = new URLSearchParams(window.location.search);
  const flow = params.get("flow");
  if (flow !== "verify" && flow !== "reset") return null;

  return { flow, code: params.get("code") ?? "" };
};

// Relay for a login page that was opened as a popup and has no redirect_path
// of its own to follow: hand completion to the same-origin opener one level
// at a time and step out of the way.
const notifyPopupOpener = (): boolean => {
  if (new URLSearchParams(window.location.search).get(popupFlowParam) !== "1") return false;

  const opener = window.opener;
  if (!opener || opener.closed) return false;

  try {
    if (opener.location.origin !== window.location.origin) return false;
  } catch {
    return false;
  }

  opener.postMessage(loginSuccessMessage, window.location.origin);
  opener.focus();
  window.close();

  return true;
};

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

export class TurnaLogin {
  readonly base: URL;

  constructor(options?: CreateLoginOptions) {
    if (options?.base) {
      const base = new URL(options.base, window.location.origin);
      // a base without a trailing slash would drop its last path segment
      // during relative resolution
      if (!base.pathname.endsWith("/")) {
        base.pathname += "/";
      }

      this.base = base;
      return;
    }

    // Served at {base}auth/sdk.js: one directory up from the module is the
    // login base path. When the SDK source is bundled elsewhere,
    // import.meta.url points at the bundle instead; pass `base` explicitly
    // in that case. The indirection keeps bundlers from statically
    // rewriting the `new URL(..., import.meta.url)` pattern.
    const moduleURL: string = import.meta.url;
    this.base = new URL("../", moduleURL);
  }

  private authURL(path: string): URL {
    return new URL(`auth/${path}`, this.base);
  }

  private methodsURL(): URL {
    // When `path.methods` is overridden in the login middleware config, the
    // server rewrites this marker with the configured route while serving
    // sdk.js, so the SDK keeps working with custom paths.
    const methodsPathOverride = "__TURNA_METHODS_PATH__";
    if (!methodsPathOverride.startsWith("__TURNA_")) {
      return new URL(methodsPathOverride, window.location.origin);
    }

    return this.authURL("methods");
  }

  /**
   * Fetch the login method manifest. Render one control per link; `name`
   * is a display label and is not guaranteed to be unique.
   */
  async methods(): Promise<LoginMethods> {
    const response = await request(this.methodsURL());
    const data = await response.json();

    return (data?.payload ?? data) as LoginMethods;
  }

  /** Password sign-in. Resolves when the session cookie is set. */
  async password(link: LoginLink, options: PasswordOptions): Promise<void> {
    try {
      await postJSON(link.url, {
        ...options.extra,
        username: options.username,
        password: options.password,
        remember_me: options.rememberMe ?? false,
      });
    } catch (reason) {
      throw asLoginError(reason);
    }
  }

  /**
   * Passkey (WebAuthn) sign-in: begin request, browser ceremony, finish
   * request. Without `username` the discoverable flow lets the browser
   * offer every resident passkey for this site — no account name typed.
   * Resolves when the session cookie is set.
   */
  async passkey(link: LoginLink, options?: PasskeyOptions): Promise<void> {
    if (!isWebAuthnSupported()) {
      throw new LoginError("Passkeys are not supported in this browser");
    }

    const rememberMe = options?.rememberMe ?? false;

    try {
      const begin: { session_id: string; options: ServerRequestOptions } = await postJSON(
        link.url,
        {
          ...(options?.username ? { username: options.username } : {}),
          remember_me: rememberMe,
        },
      );

      const assertion = await startAuthentication(begin.options);
      if (!assertion) {
        throw new LoginError("Passkey ceremony was cancelled");
      }

      // remember_me is sent on both requests; the finish request controls
      // the issued session.
      await postJSON(link.url, {
        session_id: begin.session_id,
        assertion,
        remember_me: rememberMe,
      });
    } catch (reason) {
      throw asLoginError(reason);
    }
  }

  /**
   * OAuth2 authorization-code sign-in through a popup window. Resolves
   * when the callback marks the login as complete, rejects when the popup
   * is blocked or `signal` aborts.
   *
   * A `turna:login:success` message from the exact popup triggers an
   * `auth_verify_<flow>` cookie check. Polling the same cookie keeps the
   * flow working when an upstream provider's COOP policy severs the
   * `window.opener` handle.
   *
   * The cookie is scoped to a per-call flow id: the popup may itself be a
   * login page that opens further popups, and a shared marker would let
   * such an inner sign-in resolve this call early, closing the popup while
   * it is still redirecting.
   */
  code(link: LoginLink, options?: CodeOptions): Promise<void> {
    const flowID = newFlowID();
    const flowCookie = `${successCookie}_${flowID}`;

    const target = new URL(link.url, window.location.origin);
    if (options?.rememberMe) target.searchParams.set("remember_me", "true");
    target.searchParams.set(popupFlowParam, "1");
    target.searchParams.set(flowIDParam, flowID);

    const win = window.open(
      target.toString(),
      options?.target ?? "_blank",
      options?.features ?? defaultPopupFeatures(),
    );
    if (!win) {
      return Promise.reject(
        new LoginError("The sign-in window was blocked. Allow pop-ups and try again."),
      );
    }

    return new Promise<void>((resolve, reject) => {
      let timer: ReturnType<typeof setInterval> | undefined;

      const cleanup = () => {
        if (timer) clearInterval(timer);
        window.removeEventListener("message", onMessage);
        options?.signal?.removeEventListener("abort", onAbort);
      };

      const finish = (): boolean => {
        if (readCookie(flowCookie) !== "true") return false;

        cleanup();
        dropCookie(flowCookie);
        win.close();
        // the popup focuses its opener before closing, but a completion
        // seen only through the cookie poll has nothing to hand focus back
        try {
          window.focus();
        } catch {
          // focus is a hint, never fail the sign-in over it
        }
        resolve();

        return true;
      };

      const onMessage = (event: MessageEvent) => {
        if (
          event.origin !== window.location.origin ||
          event.source !== win ||
          event.data !== loginSuccessMessage
        ) {
          return;
        }

        finish();
      };

      const onAbort = () => {
        cleanup();
        dropCookie(flowCookie);
        win.close();
        reject(new LoginError("Sign-in was aborted"));
      };

      window.addEventListener("message", onMessage);
      options?.signal?.addEventListener("abort", onAbort);

      let closedChecks = 0;
      timer = setInterval(() => {
        // The cookie is authoritative; a COOP provider may report
        // win.closed=true while authentication is still in progress, so a
        // closed popup never rejects — a late login can still complete.
        if (finish()) return;

        if (win.closed) {
          closedChecks += 1;
          if (closedChecks === 10) {
            options?.onPopupClosed?.();
          }
        }
      }, 500);
    });
  }

  /** Self-registration through the provider's `signup_url`. */
  async signup(link: LoginLink, options: SignupOptions): Promise<SignupResult> {
    if (!link.signup_url) {
      throw new LoginError("Provider does not support signup");
    }

    try {
      const data = await postJSON(link.signup_url, {
        name: options.name,
        email: options.email,
        password: options.password,
        redirect_uri: options.redirectUri ?? pageURL("verify"),
      });
      const payload = data?.payload ?? {};

      return {
        message: payload.message ?? "Account request accepted",
        verificationRequired: !!payload.verification_required,
      };
    } catch (reason) {
      throw asLoginError(reason);
    }
  }

  /** Confirm an emailed signup verification code. */
  async signupVerify(link: LoginLink, code: string): Promise<string> {
    if (!link.signup_verify_url) {
      throw new LoginError("Provider does not support signup verification");
    }

    try {
      const data = await postJSON(link.signup_verify_url, { code });

      return data?.payload?.message ?? "Email verified";
    } catch (reason) {
      throw asLoginError(reason);
    }
  }

  /** Request a forgot-password mail through `password_reset_url`. */
  async resetRequest(link: LoginLink, options: ResetRequestOptions): Promise<string> {
    if (!link.password_reset_url) {
      throw new LoginError("Provider does not support password reset");
    }

    try {
      const data = await postJSON(link.password_reset_url, {
        email: options.email,
        redirect_uri: options.redirectUri ?? pageURL("reset"),
      });

      return data?.payload?.message ?? "Check your email";
    } catch (reason) {
      throw asLoginError(reason);
    }
  }

  /** Set a new password with an emailed reset code. */
  async resetConfirm(link: LoginLink, code: string, password: string): Promise<string> {
    if (!link.password_reset_confirm_url) {
      throw new LoginError("Provider does not support password reset");
    }

    try {
      const data = await postJSON(link.password_reset_confirm_url, { code, password });

      return data?.payload?.message ?? "Password updated";
    } catch (reason) {
      throw asLoginError(reason);
    }
  }

  /**
   * Complete a successful sign-in: reload the page when it is part of
   * Turna's own authorization-code flow (so the middleware can return the
   * pending code), follow a safe `redirect_path`, or — for a login page
   * opened as a popup with nothing left to follow — relay completion to
   * its same-origin opener and close.
   */
  finish(): void {
    if (isResponseTypeCode()) {
      window.location.replace(window.location.href);
      return;
    }

    // A pending redirect_path always wins: an intermediate login page in a
    // nested flow is opened as a popup *and* carries the authorize URL it
    // has to walk back through. Relaying and closing here would strand the
    // outer window waiting for a callback that never arrives.
    const redirectPath = safeRedirectPath();
    if (redirectPath) {
      window.location.assign(redirectPath);
      return;
    }

    if (notifyPopupOpener()) return;

    window.location.assign("/");
  }
}

/** Create a login client. See {@link CreateLoginOptions} for base-path rules. */
export const createLogin = (options?: CreateLoginOptions): TurnaLogin => new TurnaLogin(options);
