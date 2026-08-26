var $ = Object.defineProperty;
var T = (r, e, t) => e in r ? $(r, e, { enumerable: !0, configurable: !0, writable: !0, value: t }) : r[e] = t;
var l = (r, e, t) => T(r, typeof e != "symbol" ? e + "" : e, t);
function u(r) {
  const e = r instanceof Uint8Array ? r : new Uint8Array(r);
  let t = "";
  for (let a = 0; a < e.length; a++) t += String.fromCharCode(e[a]);
  return btoa(t).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function _(r) {
  const e = r.replace(/-/g, "+").replace(/_/g, "/").padEnd(r.length + (4 - r.length % 4) % 4, "="), t = atob(e), a = new ArrayBuffer(t.length), n = new Uint8Array(a);
  for (let o = 0; o < t.length; o++) n[o] = t.charCodeAt(o);
  return n;
}
function R() {
  return typeof window < "u" && typeof window.PublicKeyCredential < "u";
}
async function M(r) {
  if (!R()) throw new Error("webauthn not supported");
  const e = {
    challenge: _(r.challenge),
    timeout: r.timeout,
    rpId: r.rpId,
    allowCredentials: (r.allowCredentials ?? []).map((n) => ({
      type: n.type,
      id: _(n.id),
      transports: n.transports
    })),
    userVerification: r.userVerification
  }, t = await navigator.credentials.get({ publicKey: e });
  if (!t) return null;
  const a = t.response;
  return {
    id: t.id,
    rawId: u(t.rawId),
    type: "public-key",
    response: {
      clientDataJSON: u(a.clientDataJSON),
      authenticatorData: u(a.authenticatorData),
      signature: u(a.signature),
      userHandle: a.userHandle ? u(a.userHandle) : void 0
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    authenticatorAttachment: t.authenticatorAttachment
  };
}
class s extends Error {
  constructor(t, a) {
    super(t);
    /** HTTP status when the failure came from a response. */
    l(this, "status");
    /** True for wrong username/password/secret style failures. */
    l(this, "credentials");
    this.name = "LoginError", this.status = a?.status, this.credentials = a?.credentials ?? !1;
  }
}
const k = ["password not match", "user not found", "secret not match"], b = "Invalid username or password", U = "turna:login:success", v = "turna_popup", I = "turna_flow", O = "auth_verify", D = async (r) => {
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
  return typeof e == "string" && e ? k.includes(e.toLowerCase()) ? new s(b, { status: r.status, credentials: !0 }) : new s(e, { status: r.status }) : r.status === 401 ? new s(b, { status: 401, credentials: !0 }) : new s(`Request failed with status ${r.status}`, {
    status: r.status
  });
}, c = (r) => r instanceof s ? r : r instanceof Error ? r.name === "NotAllowedError" ? new s("Passkey was cancelled or timed out") : new s(r.message) : new s(String(r)), S = async (r, e) => {
  let t;
  try {
    t = await fetch(r, { credentials: "same-origin", ...e });
  } catch (a) {
    throw c(a);
  }
  if (!t.ok)
    throw await D(t);
  return t;
}, i = async (r, e) => {
  const t = await S(r, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(e)
  });
  if (t.status !== 204)
    return t.json().catch(() => {
    });
}, N = (r) => {
  for (const e of document.cookie.split("; ")) {
    const t = e.indexOf("=");
    if (t > 0 && e.slice(0, t) === r)
      return decodeURIComponent(e.slice(t + 1));
  }
}, P = (r) => {
  document.cookie = `${r}=; Max-Age=0; path=/; SameSite=Lax`;
}, j = () => {
  const r = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues)
    globalThis.crypto.getRandomValues(r);
  else
    for (let e = 0; e < r.length; e += 1) r[e] = Math.floor(Math.random() * 256);
  return Array.from(r, (e) => e.toString(16).padStart(2, "0")).join("");
}, L = (r) => `${window.location.origin}${window.location.pathname}?flow=${r}`, q = () => new URLSearchParams(window.location.search).get("response_type") === "code", A = () => {
  const r = new URLSearchParams(window.location.search).get("redirect_path");
  if (r?.startsWith("/") && !r.startsWith("//"))
    try {
      const e = new URL(r, window.location.origin), t = e.pathname.replace(/\/+$/, "") || "/", a = window.location.pathname.replace(/\/+$/, "") || "/";
      if (e.origin === window.location.origin && t !== a)
        return `${e.pathname}${e.search}${e.hash}`;
    } catch {
    }
}, J = () => A() ?? "/", V = () => {
  const r = new URLSearchParams(window.location.search), e = r.get("flow");
  return e !== "verify" && e !== "reset" ? null : { flow: e, code: r.get("code") ?? "" };
}, x = () => {
  if (new URLSearchParams(window.location.search).get(v) !== "1") return !1;
  const r = window.opener;
  if (!r || r.closed) return !1;
  try {
    if (r.location.origin !== window.location.origin) return !1;
  } catch {
    return !1;
  }
  return r.postMessage(U, window.location.origin), r.focus(), window.close(), !0;
};
class W {
  constructor(e) {
    l(this, "base");
    if (e?.base) {
      const a = new URL(e.base, window.location.origin);
      a.pathname.endsWith("/") || (a.pathname += "/"), this.base = a;
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
    const t = await (await S(this.methodsURL())).json();
    return t?.payload ?? t;
  }
  /** Password sign-in. Resolves when the session cookie is set. */
  async password(e, t) {
    try {
      await i(e.url, {
        ...t.extra,
        username: t.username,
        password: t.password,
        remember_me: t.rememberMe ?? !1
      });
    } catch (a) {
      throw c(a);
    }
  }
  /**
   * Passkey (WebAuthn) sign-in: begin request, browser ceremony, finish
   * request. Without `username` the discoverable flow lets the browser
   * offer every resident passkey for this site — no account name typed.
   * Resolves when the session cookie is set.
   */
  async passkey(e, t) {
    if (!R())
      throw new s("Passkeys are not supported in this browser");
    const a = t?.rememberMe ?? !1;
    try {
      const n = await i(
        e.url,
        {
          ...t?.username ? { username: t.username } : {},
          remember_me: a
        }
      ), o = await M(n.options);
      if (!o)
        throw new s("Passkey ceremony was cancelled");
      await i(e.url, {
        session_id: n.session_id,
        assertion: o,
        remember_me: a
      });
    } catch (n) {
      throw c(n);
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
    const a = j(), n = `${O}_${a}`, o = new URL(e.url, window.location.origin);
    t?.rememberMe && o.searchParams.set("remember_me", "true"), o.searchParams.set(v, "1"), o.searchParams.set(I, a);
    const d = window.open(o.toString(), t?.target ?? "_blank", t?.features);
    return d ? new Promise((C, E) => {
      let w;
      const f = () => {
        w && clearInterval(w), window.removeEventListener("message", m), t?.signal?.removeEventListener("abort", g);
      }, p = () => {
        if (N(n) !== "true") return !1;
        f(), P(n), d.close();
        try {
          window.focus();
        } catch {
        }
        return C(), !0;
      }, m = (h) => {
        h.origin !== window.location.origin || h.source !== d || h.data !== U || p();
      }, g = () => {
        f(), P(n), d.close(), E(new s("Sign-in was aborted"));
      };
      window.addEventListener("message", m), t?.signal?.addEventListener("abort", g);
      let y = 0;
      w = setInterval(() => {
        p() || d.closed && (y += 1, y === 10 && t?.onPopupClosed?.());
      }, 500);
    }) : Promise.reject(
      new s("The sign-in window was blocked. Allow pop-ups and try again.")
    );
  }
  /** Self-registration through the provider's `signup_url`. */
  async signup(e, t) {
    if (!e.signup_url)
      throw new s("Provider does not support signup");
    try {
      const n = (await i(e.signup_url, {
        name: t.name,
        email: t.email,
        password: t.password,
        redirect_uri: t.redirectUri ?? L("verify")
      }))?.payload ?? {};
      return {
        message: n.message ?? "Account request accepted",
        verificationRequired: !!n.verification_required
      };
    } catch (a) {
      throw c(a);
    }
  }
  /** Confirm an emailed signup verification code. */
  async signupVerify(e, t) {
    if (!e.signup_verify_url)
      throw new s("Provider does not support signup verification");
    try {
      return (await i(e.signup_verify_url, { code: t }))?.payload?.message ?? "Email verified";
    } catch (a) {
      throw c(a);
    }
  }
  /** Request a forgot-password mail through `password_reset_url`. */
  async resetRequest(e, t) {
    if (!e.password_reset_url)
      throw new s("Provider does not support password reset");
    try {
      return (await i(e.password_reset_url, {
        email: t.email,
        redirect_uri: t.redirectUri ?? L("reset")
      }))?.payload?.message ?? "Check your email";
    } catch (a) {
      throw c(a);
    }
  }
  /** Set a new password with an emailed reset code. */
  async resetConfirm(e, t, a) {
    if (!e.password_reset_confirm_url)
      throw new s("Provider does not support password reset");
    try {
      return (await i(e.password_reset_confirm_url, { code: t, password: a }))?.payload?.message ?? "Password updated";
    } catch (n) {
      throw c(n);
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
    if (q()) {
      window.location.replace(window.location.href);
      return;
    }
    const e = A();
    if (e) {
      window.location.assign(e);
      return;
    }
    x() || window.location.assign("/");
  }
}
const B = (r) => new W(r);
export {
  s as LoginError,
  W as TurnaLogin,
  B as createLogin,
  V as flowFromURL,
  J as getRedirectPath,
  q as isResponseTypeCode,
  R as isWebAuthnSupported
};
