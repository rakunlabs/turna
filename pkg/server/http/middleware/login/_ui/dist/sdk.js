var S = Object.defineProperty;
var A = (r, t, e) => t in r ? S(r, t, { enumerable: !0, configurable: !0, writable: !0, value: e }) : r[t] = e;
var u = (r, t, e) => A(r, typeof t != "symbol" ? t + "" : t, e);
function d(r) {
  const t = r instanceof Uint8Array ? r : new Uint8Array(r);
  let e = "";
  for (let a = 0; a < t.length; a++) e += String.fromCharCode(t[a]);
  return btoa(e).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function y(r) {
  const t = r.replace(/-/g, "+").replace(/_/g, "/").padEnd(r.length + (4 - r.length % 4) % 4, "="), e = atob(t), a = new ArrayBuffer(e.length), n = new Uint8Array(a);
  for (let o = 0; o < e.length; o++) n[o] = e.charCodeAt(o);
  return n;
}
function L() {
  return typeof window < "u" && typeof window.PublicKeyCredential < "u";
}
async function E(r) {
  if (!L()) throw new Error("webauthn not supported");
  const t = {
    challenge: y(r.challenge),
    timeout: r.timeout,
    rpId: r.rpId,
    allowCredentials: (r.allowCredentials ?? []).map((n) => ({
      type: n.type,
      id: y(n.id),
      transports: n.transports
    })),
    userVerification: r.userVerification
  }, e = await navigator.credentials.get({ publicKey: t });
  if (!e) return null;
  const a = e.response;
  return {
    id: e.id,
    rawId: d(e.rawId),
    type: "public-key",
    response: {
      clientDataJSON: d(a.clientDataJSON),
      authenticatorData: d(a.authenticatorData),
      signature: d(a.signature),
      userHandle: a.userHandle ? d(a.userHandle) : void 0
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    authenticatorAttachment: e.authenticatorAttachment
  };
}
class s extends Error {
  constructor(e, a) {
    super(e);
    /** HTTP status when the failure came from a response. */
    u(this, "status");
    /** True for wrong username/password/secret style failures. */
    u(this, "credentials");
    this.name = "LoginError", this.status = a?.status, this.credentials = a?.credentials ?? !1;
  }
}
const C = ["password not match", "user not found", "secret not match"], _ = "Invalid username or password", P = "turna:login:success", R = "turna_popup", T = async (r) => {
  let t;
  try {
    const e = await r.json();
    e && typeof e == "object" ? t = e.message ?? e.error_description ?? e.error : typeof e == "string" && (t = e);
  } catch {
  }
  if (typeof t == "string" && t.trim().startsWith("{"))
    try {
      const e = JSON.parse(t);
      t = e.error_description ?? e.message ?? e.error ?? t;
    } catch {
    }
  return typeof t == "string" && t ? C.includes(t.toLowerCase()) ? new s(_, { status: r.status, credentials: !0 }) : new s(t, { status: r.status }) : r.status === 401 ? new s(_, { status: 401, credentials: !0 }) : new s(`Request failed with status ${r.status}`, {
    status: r.status
  });
}, c = (r) => r instanceof s ? r : r instanceof Error ? r.name === "NotAllowedError" ? new s("Passkey was cancelled or timed out") : new s(r.message) : new s(String(r)), U = async (r, t) => {
  let e;
  try {
    e = await fetch(r, { credentials: "same-origin", ...t });
  } catch (a) {
    throw c(a);
  }
  if (!e.ok)
    throw await T(e);
  return e;
}, i = async (r, t) => {
  const e = await U(r, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(t)
  });
  if (e.status !== 204)
    return e.json().catch(() => {
    });
}, $ = (r) => {
  for (const t of document.cookie.split("; ")) {
    const e = t.indexOf("=");
    if (e > 0 && t.slice(0, e) === r)
      return decodeURIComponent(t.slice(e + 1));
  }
}, b = (r) => `${window.location.origin}${window.location.pathname}?flow=${r}`, O = () => new URLSearchParams(window.location.search).get("response_type") === "code", M = () => {
  const r = new URLSearchParams(window.location.search).get("redirect_path");
  if (r?.startsWith("/") && !r.startsWith("//"))
    try {
      const t = new URL(r, window.location.origin), e = t.pathname.replace(/\/+$/, "") || "/", a = window.location.pathname.replace(/\/+$/, "") || "/";
      if (t.origin === window.location.origin && e !== a)
        return `${t.pathname}${t.search}${t.hash}`;
    } catch {
    }
  return "/";
}, q = () => {
  const r = new URLSearchParams(window.location.search), t = r.get("flow");
  return t !== "verify" && t !== "reset" ? null : { flow: t, code: r.get("code") ?? "" };
}, I = () => {
  if (new URLSearchParams(window.location.search).get(R) !== "1") return !1;
  const r = window.opener;
  if (!r || r.closed) return !1;
  try {
    if (r.location.origin !== window.location.origin) return !1;
  } catch {
    return !1;
  }
  return r.postMessage(P, window.location.origin), r.focus(), window.close(), !0;
};
class N {
  constructor(t) {
    u(this, "base");
    if (t?.base) {
      const a = new URL(t.base, window.location.origin);
      a.pathname.endsWith("/") || (a.pathname += "/"), this.base = a;
      return;
    }
    const e = import.meta.url;
    this.base = new URL("../", e);
  }
  authURL(t) {
    return new URL(`auth/${t}`, this.base);
  }
  methodsURL() {
    const t = "__TURNA_METHODS_PATH__";
    return t.startsWith("__TURNA_") ? this.authURL("methods") : new URL(t, window.location.origin);
  }
  /**
   * Fetch the login method manifest. Render one control per link; `name`
   * is a display label and is not guaranteed to be unique.
   */
  async methods() {
    const e = await (await U(this.methodsURL())).json();
    return e?.payload ?? e;
  }
  /** Password sign-in. Resolves when the session cookie is set. */
  async password(t, e) {
    try {
      await i(t.url, {
        ...e.extra,
        username: e.username,
        password: e.password,
        remember_me: e.rememberMe ?? !1
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
  async passkey(t, e) {
    if (!L())
      throw new s("Passkeys are not supported in this browser");
    const a = e?.rememberMe ?? !1;
    try {
      const n = await i(
        t.url,
        {
          ...e?.username ? { username: e.username } : {},
          remember_me: a
        }
      ), o = await E(n.options);
      if (!o)
        throw new s("Passkey ceremony was cancelled");
      await i(t.url, {
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
   * `auth_verify` cookie check. Polling the same cookie keeps the flow
   * working when an upstream provider's COOP policy severs the
   * `window.opener` handle.
   */
  code(t, e) {
    const a = new URL(t.url, window.location.origin);
    e?.rememberMe && a.searchParams.set("remember_me", "true"), a.searchParams.set(R, "1");
    const n = window.open(a.toString(), e?.target ?? "_blank", e?.features);
    return n ? new Promise((o, v) => {
      let w;
      const h = () => {
        w && clearInterval(w), window.removeEventListener("message", p), e?.signal?.removeEventListener("abort", m);
      }, f = () => $("auth_verify") !== "true" ? !1 : (h(), n.close(), o(), !0), p = (l) => {
        l.origin !== window.location.origin || l.source !== n || l.data !== P || f();
      }, m = () => {
        h(), n.close(), v(new s("Sign-in was aborted"));
      };
      window.addEventListener("message", p), e?.signal?.addEventListener("abort", m);
      let g = 0;
      w = setInterval(() => {
        f() || n.closed && (g += 1, g === 10 && e?.onPopupClosed?.());
      }, 500);
    }) : Promise.reject(
      new s("The sign-in window was blocked. Allow pop-ups and try again.")
    );
  }
  /** Self-registration through the provider's `signup_url`. */
  async signup(t, e) {
    if (!t.signup_url)
      throw new s("Provider does not support signup");
    try {
      const n = (await i(t.signup_url, {
        name: e.name,
        email: e.email,
        password: e.password,
        redirect_uri: e.redirectUri ?? b("verify")
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
  async signupVerify(t, e) {
    if (!t.signup_verify_url)
      throw new s("Provider does not support signup verification");
    try {
      return (await i(t.signup_verify_url, { code: e }))?.payload?.message ?? "Email verified";
    } catch (a) {
      throw c(a);
    }
  }
  /** Request a forgot-password mail through `password_reset_url`. */
  async resetRequest(t, e) {
    if (!t.password_reset_url)
      throw new s("Provider does not support password reset");
    try {
      return (await i(t.password_reset_url, {
        email: e.email,
        redirect_uri: e.redirectUri ?? b("reset")
      }))?.payload?.message ?? "Check your email";
    } catch (a) {
      throw c(a);
    }
  }
  /** Set a new password with an emailed reset code. */
  async resetConfirm(t, e, a) {
    if (!t.password_reset_confirm_url)
      throw new s("Provider does not support password reset");
    try {
      return (await i(t.password_reset_confirm_url, { code: e, password: a }))?.payload?.message ?? "Password updated";
    } catch (n) {
      throw c(n);
    }
  }
  /**
   * Complete a successful sign-in: reload the page when it is part of
   * Turna's own authorization-code flow (so the middleware can return the
   * pending code), relay an SDK-opened nested popup to its same-origin
   * opener, or navigate to the safe `redirect_path` target.
   */
  finish() {
    if (O()) {
      window.location.replace(window.location.href);
      return;
    }
    I() || window.location.assign(M());
  }
}
const j = (r) => new N(r);
export {
  s as LoginError,
  N as TurnaLogin,
  j as createLogin,
  q as flowFromURL,
  M as getRedirectPath,
  O as isResponseTypeCode,
  L as isWebAuthnSupported
};
