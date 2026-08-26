var E = Object.defineProperty;
var M = (r, e, t) => e in r ? E(r, e, { enumerable: !0, configurable: !0, writable: !0, value: t }) : r[e] = t;
var w = (r, e, t) => M(r, typeof e != "symbol" ? e + "" : e, t);
function u(r) {
  const e = r instanceof Uint8Array ? r : new Uint8Array(r);
  let t = "";
  for (let n = 0; n < e.length; n++) t += String.fromCharCode(e[n]);
  return btoa(t).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function b(r) {
  const e = r.replace(/-/g, "+").replace(/_/g, "/").padEnd(r.length + (4 - r.length % 4) % 4, "="), t = atob(e), n = new ArrayBuffer(t.length), a = new Uint8Array(n);
  for (let s = 0; s < t.length; s++) a[s] = t.charCodeAt(s);
  return a;
}
function U() {
  return typeof window < "u" && typeof window.PublicKeyCredential < "u";
}
async function T(r) {
  if (!U()) throw new Error("webauthn not supported");
  const e = {
    challenge: b(r.challenge),
    timeout: r.timeout,
    rpId: r.rpId,
    allowCredentials: (r.allowCredentials ?? []).map((a) => ({
      type: a.type,
      id: b(a.id),
      transports: a.transports
    })),
    userVerification: r.userVerification
  }, t = await navigator.credentials.get({ publicKey: e });
  if (!t) return null;
  const n = t.response;
  return {
    id: t.id,
    rawId: u(t.rawId),
    type: "public-key",
    response: {
      clientDataJSON: u(n.clientDataJSON),
      authenticatorData: u(n.authenticatorData),
      signature: u(n.signature),
      userHandle: n.userHandle ? u(n.userHandle) : void 0
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    authenticatorAttachment: t.authenticatorAttachment
  };
}
class o extends Error {
  constructor(t, n) {
    super(t);
    /** HTTP status when the failure came from a response. */
    w(this, "status");
    /** True for wrong username/password/secret style failures. */
    w(this, "credentials");
    this.name = "LoginError", this.status = n?.status, this.credentials = n?.credentials ?? !1;
  }
}
const k = ["password not match", "user not found", "secret not match"], P = "Invalid username or password", v = "turna:login:success", S = "turna_popup", I = "turna_flow", O = "auth_verify", x = async (r) => {
  let e;
  try {
    const t = await r.json();
    t && typeof t == "object" ? e = t.message ?? t.error_description ?? t.error : typeof t == "string" && (e = t);
  } catch {
  }
  if (typeof e == "string" && e.trim().startsWith("{"))
    try {
      const t = JSON.parse(e);
      e = t.error_description ?? t.message ?? t.error ?? e;
    } catch {
    }
  return typeof e == "string" && e ? k.includes(e.toLowerCase()) ? new o(P, { status: r.status, credentials: !0 }) : new o(e, { status: r.status }) : r.status === 401 ? new o(P, { status: 401, credentials: !0 }) : new o(`Request failed with status ${r.status}`, {
    status: r.status
  });
}, d = (r) => r instanceof o ? r : r instanceof Error ? r.name === "NotAllowedError" ? new o("Passkey was cancelled or timed out") : new o(r.message) : new o(String(r)), A = async (r, e) => {
  let t;
  try {
    t = await fetch(r, { credentials: "same-origin", ...e });
  } catch (n) {
    throw d(n);
  }
  if (!t.ok)
    throw await x(t);
  return t;
}, c = async (r, e) => {
  const t = await A(r, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(e)
  });
  if (t.status !== 204)
    return t.json().catch(() => {
    });
}, D = (r) => {
  for (const e of document.cookie.split("; ")) {
    const t = e.indexOf("=");
    if (t > 0 && e.slice(0, t) === r)
      return decodeURIComponent(e.slice(t + 1));
  }
}, L = (r) => {
  document.cookie = `${r}=; Max-Age=0; path=/; SameSite=Lax`;
}, N = () => {
  const t = window.outerWidth || screen.width, n = window.outerHeight || screen.height, a = window.screenX ?? 0, s = window.screenY ?? 0, i = Math.max(0, Math.round(a + (t - 520) / 2)), l = Math.max(0, Math.round(s + (n - 720) / 2));
  return `popup=yes,width=520,height=720,left=${i},top=${l}`;
}, W = () => {
  const r = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues)
    globalThis.crypto.getRandomValues(r);
  else
    for (let e = 0; e < r.length; e += 1) r[e] = Math.floor(Math.random() * 256);
  return Array.from(r, (e) => e.toString(16).padStart(2, "0")).join("");
}, R = (r) => `${window.location.origin}${window.location.pathname}?flow=${r}`, j = () => new URLSearchParams(window.location.search).get("response_type") === "code", C = () => {
  const r = new URLSearchParams(window.location.search).get("redirect_path");
  if (r?.startsWith("/") && !r.startsWith("//"))
    try {
      const e = new URL(r, window.location.origin), t = e.pathname.replace(/\/+$/, "") || "/", n = window.location.pathname.replace(/\/+$/, "") || "/";
      if (e.origin === window.location.origin && t !== n)
        return `${e.pathname}${e.search}${e.hash}`;
    } catch {
    }
}, V = () => C() ?? "/", F = () => {
  const r = new URLSearchParams(window.location.search), e = r.get("flow");
  return e !== "verify" && e !== "reset" ? null : { flow: e, code: r.get("code") ?? "" };
}, q = () => {
  if (new URLSearchParams(window.location.search).get(S) !== "1") return !1;
  const r = window.opener;
  if (!r || r.closed) return !1;
  try {
    if (r.location.origin !== window.location.origin) return !1;
  } catch {
    return !1;
  }
  return r.postMessage(v, window.location.origin), r.focus(), window.close(), !0;
};
class H {
  constructor(e) {
    w(this, "base");
    if (e?.base) {
      const n = new URL(e.base, window.location.origin);
      n.pathname.endsWith("/") || (n.pathname += "/"), this.base = n;
      return;
    }
    const t = import.meta.url;
    this.base = new URL("../", t);
  }
  authURL(e) {
    return new URL(`auth/${e}`, this.base);
  }
  methodsURL() {
    const e = "__TURNA_METHODS_PATH__";
    return e.startsWith("__TURNA_") ? this.authURL("methods") : new URL(e, window.location.origin);
  }
  /**
   * Fetch the login method manifest. Render one control per link; `name`
   * is a display label and is not guaranteed to be unique.
   */
  async methods() {
    const t = await (await A(this.methodsURL())).json();
    return t?.payload ?? t;
  }
  /** Password sign-in. Resolves when the session cookie is set. */
  async password(e, t) {
    try {
      await c(e.url, {
        ...t.extra,
        username: t.username,
        password: t.password,
        remember_me: t.rememberMe ?? !1
      });
    } catch (n) {
      throw d(n);
    }
  }
  /**
   * Passkey (WebAuthn) sign-in: begin request, browser ceremony, finish
   * request. Without `username` the discoverable flow lets the browser
   * offer every resident passkey for this site — no account name typed.
   * Resolves when the session cookie is set.
   */
  async passkey(e, t) {
    if (!U())
      throw new o("Passkeys are not supported in this browser");
    const n = t?.rememberMe ?? !1;
    try {
      const a = await c(
        e.url,
        {
          ...t?.username ? { username: t.username } : {},
          remember_me: n
        }
      ), s = await T(a.options);
      if (!s)
        throw new o("Passkey ceremony was cancelled");
      await c(e.url, {
        session_id: a.session_id,
        assertion: s,
        remember_me: n
      });
    } catch (a) {
      throw d(a);
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
  code(e, t) {
    const n = W(), a = `${O}_${n}`, s = new URL(e.url, window.location.origin);
    t?.rememberMe && s.searchParams.set("remember_me", "true"), s.searchParams.set(S, "1"), s.searchParams.set(I, n);
    const i = window.open(
      s.toString(),
      t?.target ?? "_blank",
      t?.features ?? N()
    );
    return i ? new Promise((l, $) => {
      let h;
      const p = () => {
        h && clearInterval(h), window.removeEventListener("message", g), t?.signal?.removeEventListener("abort", y);
      }, m = () => {
        if (D(a) !== "true") return !1;
        p(), L(a), i.close();
        try {
          window.focus();
        } catch {
        }
        return l(), !0;
      }, g = (f) => {
        f.origin !== window.location.origin || f.source !== i || f.data !== v || m();
      }, y = () => {
        p(), L(a), i.close(), $(new o("Sign-in was aborted"));
      };
      window.addEventListener("message", g), t?.signal?.addEventListener("abort", y);
      let _ = 0;
      h = setInterval(() => {
        m() || i.closed && (_ += 1, _ === 10 && t?.onPopupClosed?.());
      }, 500);
    }) : Promise.reject(
      new o("The sign-in window was blocked. Allow pop-ups and try again.")
    );
  }
  /** Self-registration through the provider's `signup_url`. */
  async signup(e, t) {
    if (!e.signup_url)
      throw new o("Provider does not support signup");
    try {
      const a = (await c(e.signup_url, {
        name: t.name,
        email: t.email,
        password: t.password,
        redirect_uri: t.redirectUri ?? R("verify")
      }))?.payload ?? {};
      return {
        message: a.message ?? "Account request accepted",
        verificationRequired: !!a.verification_required
      };
    } catch (n) {
      throw d(n);
    }
  }
  /** Confirm an emailed signup verification code. */
  async signupVerify(e, t) {
    if (!e.signup_verify_url)
      throw new o("Provider does not support signup verification");
    try {
      return (await c(e.signup_verify_url, { code: t }))?.payload?.message ?? "Email verified";
    } catch (n) {
      throw d(n);
    }
  }
  /** Request a forgot-password mail through `password_reset_url`. */
  async resetRequest(e, t) {
    if (!e.password_reset_url)
      throw new o("Provider does not support password reset");
    try {
      return (await c(e.password_reset_url, {
        email: t.email,
        redirect_uri: t.redirectUri ?? R("reset")
      }))?.payload?.message ?? "Check your email";
    } catch (n) {
      throw d(n);
    }
  }
  /** Set a new password with an emailed reset code. */
  async resetConfirm(e, t, n) {
    if (!e.password_reset_confirm_url)
      throw new o("Provider does not support password reset");
    try {
      return (await c(e.password_reset_confirm_url, { code: t, password: n }))?.payload?.message ?? "Password updated";
    } catch (a) {
      throw d(a);
    }
  }
  /**
   * Complete a successful sign-in: reload the page when it is part of
   * Turna's own authorization-code flow (so the middleware can return the
   * pending code), follow a safe `redirect_path`, or — for a login page
   * opened as a popup with nothing left to follow — relay completion to
   * its same-origin opener and close.
   */
  finish() {
    if (j()) {
      window.location.replace(window.location.href);
      return;
    }
    const e = C();
    if (e) {
      window.location.assign(e);
      return;
    }
    q() || window.location.assign("/");
  }
}
const B = (r) => new H(r);
export {
  o as LoginError,
  H as TurnaLogin,
  B as createLogin,
  F as flowFromURL,
  V as getRedirectPath,
  j as isResponseTypeCode,
  U as isWebAuthnSupported
};
