var $ = Object.defineProperty;
var M = (r, t, e) => t in r ? $(r, t, { enumerable: !0, configurable: !0, writable: !0, value: e }) : r[t] = e;
var h = (r, t, e) => M(r, typeof t != "symbol" ? t + "" : t, e);
function d(r) {
  const t = r instanceof Uint8Array ? r : new Uint8Array(r);
  let e = "";
  for (let n = 0; n < t.length; n++) e += String.fromCharCode(t[n]);
  return btoa(e).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function l(r) {
  const t = r.replace(/-/g, "+").replace(/_/g, "/").padEnd(r.length + (4 - r.length % 4) % 4, "="), e = atob(t), n = new ArrayBuffer(e.length), a = new Uint8Array(n);
  for (let s = 0; s < e.length; s++) a[s] = e.charCodeAt(s);
  return a;
}
function w() {
  return typeof window < "u" && typeof window.PublicKeyCredential < "u";
}
async function I(r) {
  if (!w()) throw new Error("webauthn not supported");
  const t = {
    challenge: l(r.challenge),
    timeout: r.timeout,
    rpId: r.rpId,
    allowCredentials: (r.allowCredentials ?? []).map((a) => ({
      type: a.type,
      id: l(a.id),
      transports: a.transports
    })),
    userVerification: r.userVerification
  }, e = await navigator.credentials.get({ publicKey: t });
  if (!e) return null;
  const n = e.response;
  return {
    id: e.id,
    rawId: d(e.rawId),
    type: "public-key",
    response: {
      clientDataJSON: d(n.clientDataJSON),
      authenticatorData: d(n.authenticatorData),
      signature: d(n.signature),
      userHandle: n.userHandle ? d(n.userHandle) : void 0
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    authenticatorAttachment: e.authenticatorAttachment
  };
}
async function T(r) {
  if (!w()) throw new Error("webauthn not supported");
  const t = {
    challenge: l(r.challenge),
    rp: r.rp,
    user: {
      id: l(r.user.id),
      name: r.user.name,
      displayName: r.user.displayName
    },
    pubKeyCredParams: r.pubKeyCredParams,
    timeout: r.timeout,
    excludeCredentials: (r.excludeCredentials ?? []).map((a) => ({
      type: a.type,
      id: l(a.id),
      transports: a.transports
    })),
    authenticatorSelection: r.authenticatorSelection,
    attestation: r.attestation
  }, e = await navigator.credentials.create({ publicKey: t });
  if (!e) throw new Error("registration was cancelled");
  const n = e.response;
  return {
    id: e.id,
    rawId: d(e.rawId),
    type: "public-key",
    response: {
      clientDataJSON: d(n.clientDataJSON),
      attestationObject: d(n.attestationObject),
      transports: typeof n.getTransports == "function" ? n.getTransports() : void 0
    },
    authenticatorAttachment: e.authenticatorAttachment
  };
}
class o extends Error {
  constructor(e, n) {
    super(e);
    /** HTTP status when the failure came from a response. */
    h(this, "status");
    /** True for wrong username/password/secret style failures. */
    h(this, "credentials");
    this.name = "LoginError", this.status = n?.status, this.credentials = n?.credentials ?? !1;
  }
}
const N = ["password not match", "user not found", "secret not match"], L = "Invalid username or password", C = "turna:login:success", A = "turna_popup", O = "turna_flow", D = "auth_verify", U = "turna:passkey-enrollment:", x = async (r) => {
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
  return typeof t == "string" && t ? N.includes(t.toLowerCase()) ? new o(L, { status: r.status, credentials: !0 }) : new o(t, { status: r.status }) : r.status === 401 ? new o(L, { status: 401, credentials: !0 }) : new o(`Request failed with status ${r.status}`, {
    status: r.status
  });
}, c = (r) => r instanceof o ? r : r instanceof Error ? r.name === "NotAllowedError" ? new o("Passkey was cancelled or timed out") : new o(r.message) : new o(String(r)), g = async (r, t) => {
  let e;
  try {
    e = await fetch(r, { credentials: "same-origin", ...t });
  } catch (n) {
    throw c(n);
  }
  if (!e.ok)
    throw await x(e);
  return e;
}, i = async (r, t) => {
  const e = await g(r, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(t)
  });
  if (e.status !== 204)
    return e.json().catch(() => {
    });
}, j = (r) => {
  for (const t of document.cookie.split("; ")) {
    const e = t.indexOf("=");
    if (e > 0 && t.slice(0, e) === r)
      return decodeURIComponent(t.slice(e + 1));
  }
}, S = (r) => {
  document.cookie = `${r}=; Max-Age=0; path=/; SameSite=Lax`;
}, W = () => {
  const e = window.outerWidth || screen.width, n = window.outerHeight || screen.height, a = window.screenX ?? 0, s = window.screenY ?? 0, u = Math.max(0, Math.round(a + (e - 520) / 2)), p = Math.max(0, Math.round(s + (n - 720) / 2));
  return `popup=yes,width=520,height=720,left=${u},top=${p}`;
}, q = () => {
  const r = new Uint8Array(16);
  if (globalThis.crypto?.getRandomValues)
    globalThis.crypto.getRandomValues(r);
  else
    for (let t = 0; t < r.length; t += 1) r[t] = Math.floor(Math.random() * 256);
  return Array.from(r, (t) => t.toString(16).padStart(2, "0")).join("");
}, v = (r) => `${window.location.origin}${window.location.pathname}?flow=${r}`, H = () => new URLSearchParams(window.location.search).get("response_type") === "code", E = () => {
  const r = new URLSearchParams(window.location.search).get("redirect_path");
  if (r?.startsWith("/") && !r.startsWith("//"))
    try {
      const t = new URL(r, window.location.origin), e = t.pathname.replace(/\/+$/, "") || "/", n = window.location.pathname.replace(/\/+$/, "") || "/";
      if (t.origin === window.location.origin && e !== n)
        return `${t.pathname}${t.search}${t.hash}`;
    } catch {
    }
}, V = () => E() ?? "/", z = () => {
  const r = new URLSearchParams(window.location.search), t = r.get("flow");
  return t !== "verify" && t !== "reset" ? null : { flow: t, code: r.get("code") ?? "" };
}, J = () => {
  if (new URLSearchParams(window.location.search).get(A) !== "1") return !1;
  const r = window.opener;
  if (!r || r.closed) return !1;
  try {
    if (r.location.origin !== window.location.origin) return !1;
  } catch {
    return !1;
  }
  return r.postMessage(C, window.location.origin), r.focus(), window.close(), !0;
};
class F {
  constructor(t) {
    h(this, "base");
    if (t?.base) {
      const n = new URL(t.base, window.location.origin);
      n.pathname.endsWith("/") || (n.pathname += "/"), this.base = n;
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
  enrollmentURL() {
    return this.authURL("passkey/enrollment");
  }
  /**
   * Fetch the login method manifest. Render one control per link; `name`
   * is a display label and is not guaranteed to be unique.
   */
  async methods() {
    const e = await (await g(this.methodsURL())).json();
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
    } catch (n) {
      throw c(n);
    }
  }
  /**
   * Passkey (WebAuthn) sign-in: begin request, browser ceremony, finish
   * request. Without `username` the discoverable flow lets the browser
   * offer every resident passkey for this site — no account name typed.
   * Resolves when the session cookie is set.
   */
  async passkey(t, e) {
    if (!w())
      throw new o("Passkeys are not supported in this browser");
    const n = e?.rememberMe ?? !1;
    try {
      const a = await i(
        t.url,
        {
          ...e?.username ? { username: e.username } : {},
          remember_me: n
        }
      ), s = await I(a.options);
      if (!s)
        throw new o("Passkey ceremony was cancelled");
      await i(t.url, {
        session_id: a.session_id,
        assertion: s,
        remember_me: n
      });
    } catch (a) {
      throw c(a);
    }
  }
  /** Return Auth's post-login enrollment decision, honoring this browser's
   * optional snooze marker. A policy/storage failure never changes session
   * validity; callers may simply finish the successful login. */
  async passkeyEnrollmentStatus() {
    if (!w()) return { prompt: !1 };
    const e = await (await g(this.enrollmentURL())).json(), n = e?.payload ?? e;
    if (!n.prompt || !n.prompt_id) return n;
    try {
      const a = Number(localStorage.getItem(U + n.prompt_id));
      if (Number.isFinite(a) && a > Date.now())
        return { ...n, prompt: !1 };
    } catch {
    }
    return n;
  }
  /** Suppress this optional prompt for the duration selected by Auth. */
  snoozePasskeyEnrollment(t) {
    if (t.prompt_id)
      try {
        const e = Math.max(0, t.snooze_seconds ?? 0) * 1e3;
        localStorage.setItem(U + t.prompt_id, String(Date.now() + e));
      } catch {
      }
  }
  /** Register a passkey for the authenticated session. The login middleware
   * derives the target user from the validated token; no user id is sent. */
  async enrollPasskey(t) {
    if (!w())
      throw new o("Passkeys are not supported in this browser");
    try {
      const e = await i(this.enrollmentURL(), {}), n = e?.payload ?? e, a = await T(n.options);
      await i(this.enrollmentURL(), {
        session_id: n.session_id,
        name: t?.name?.trim() ?? "",
        credential: a
      });
    } catch (e) {
      throw c(e);
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
  code(t, e) {
    const n = q(), a = `${D}_${n}`, s = new URL(t.url, window.location.origin);
    e?.rememberMe && s.searchParams.set("remember_me", "true"), s.searchParams.set(A, "1"), s.searchParams.set(O, n);
    const u = window.open(
      s.toString(),
      e?.target ?? "_blank",
      e?.features ?? W()
    );
    return u ? new Promise((p, k) => {
      let m;
      const y = () => {
        m && clearInterval(m), window.removeEventListener("message", _), e?.signal?.removeEventListener("abort", P);
      }, b = () => {
        if (j(a) !== "true") return !1;
        y(), S(a), u.close();
        try {
          window.focus();
        } catch {
        }
        return p(), !0;
      }, _ = (f) => {
        f.origin !== window.location.origin || f.source !== u || f.data !== C || b();
      }, P = () => {
        y(), S(a), u.close(), k(new o("Sign-in was aborted"));
      };
      window.addEventListener("message", _), e?.signal?.addEventListener("abort", P);
      let R = 0;
      m = setInterval(() => {
        b() || u.closed && (R += 1, R === 10 && e?.onPopupClosed?.());
      }, 500);
    }) : Promise.reject(
      new o("The sign-in window was blocked. Allow pop-ups and try again.")
    );
  }
  /** Self-registration through the provider's `signup_url`. */
  async signup(t, e) {
    if (!t.signup_url)
      throw new o("Provider does not support signup");
    try {
      const a = (await i(t.signup_url, {
        name: e.name,
        email: e.email,
        password: e.password,
        redirect_uri: e.redirectUri ?? v("verify")
      }))?.payload ?? {};
      return {
        message: a.message ?? "Account request accepted",
        verificationRequired: !!a.verification_required
      };
    } catch (n) {
      throw c(n);
    }
  }
  /** Confirm an emailed signup verification code. */
  async signupVerify(t, e) {
    if (!t.signup_verify_url)
      throw new o("Provider does not support signup verification");
    try {
      return (await i(t.signup_verify_url, { code: e }))?.payload?.message ?? "Email verified";
    } catch (n) {
      throw c(n);
    }
  }
  /** Request a forgot-password mail through `password_reset_url`. */
  async resetRequest(t, e) {
    if (!t.password_reset_url)
      throw new o("Provider does not support password reset");
    try {
      return (await i(t.password_reset_url, {
        email: e.email,
        redirect_uri: e.redirectUri ?? v("reset")
      }))?.payload?.message ?? "Check your email";
    } catch (n) {
      throw c(n);
    }
  }
  /** Set a new password with an emailed reset code. */
  async resetConfirm(t, e, n) {
    if (!t.password_reset_confirm_url)
      throw new o("Provider does not support password reset");
    try {
      return (await i(t.password_reset_confirm_url, { code: e, password: n }))?.payload?.message ?? "Password updated";
    } catch (a) {
      throw c(a);
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
    if (H()) {
      window.location.replace(window.location.href);
      return;
    }
    const t = E();
    if (t) {
      window.location.assign(t);
      return;
    }
    J() || window.location.assign("/");
  }
}
const B = (r) => new F(r);
export {
  o as LoginError,
  F as TurnaLogin,
  B as createLogin,
  z as flowFromURL,
  V as getRedirectPath,
  H as isResponseTypeCode,
  w as isWebAuthnSupported
};
