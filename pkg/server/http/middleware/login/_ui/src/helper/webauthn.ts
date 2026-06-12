/**
 * Zero-dependency WebAuthn helpers for the login UI.
 *
 * The navigator.credentials API works with ArrayBuffers while the wire
 * format uses URL-safe base64 without padding (matching the server side).
 */

export function bufferToBase64URL(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function base64URLToBuffer(s: string): Uint8Array {
  const padded = s
    .replace(/-/g, "+")
    .replace(/_/g, "/")
    .padEnd(s.length + ((4 - (s.length % 4)) % 4), "=");
  const bin = atob(padded);
  const buf = new ArrayBuffer(bin.length);
  const out = new Uint8Array(buf);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export function isWebAuthnSupported(): boolean {
  return typeof window !== "undefined" && typeof window.PublicKeyCredential !== "undefined";
}

export interface ServerRequestOptions {
  challenge: string;
  timeout?: number;
  rpId: string;
  allowCredentials?: Array<{
    type: "public-key";
    id: string;
    transports?: string[];
  }>;
  userVerification?: UserVerificationRequirement;
}

export interface AssertionResponseJSON {
  id: string;
  rawId: string;
  type: "public-key";
  response: {
    clientDataJSON: string;
    authenticatorData: string;
    signature: string;
    userHandle?: string;
  };
  authenticatorAttachment?: string;
}

/** Run the login (assertion) ceremony in the browser. */
export async function startAuthentication(
  opts: ServerRequestOptions,
): Promise<AssertionResponseJSON | null> {
  if (!isWebAuthnSupported()) throw new Error("webauthn not supported");

  const publicKey: PublicKeyCredentialRequestOptions = {
    challenge: base64URLToBuffer(opts.challenge),
    timeout: opts.timeout,
    rpId: opts.rpId,
    allowCredentials: (opts.allowCredentials ?? []).map((c) => ({
      type: c.type,
      id: base64URLToBuffer(c.id),
      transports: c.transports as AuthenticatorTransport[] | undefined,
    })),
    userVerification: opts.userVerification,
  };

  const cred = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null;
  if (!cred) return null;

  const asn = cred.response as AuthenticatorAssertionResponse;
  return {
    id: cred.id,
    rawId: bufferToBase64URL(cred.rawId),
    type: "public-key",
    response: {
      clientDataJSON: bufferToBase64URL(asn.clientDataJSON),
      authenticatorData: bufferToBase64URL(asn.authenticatorData),
      signature: bufferToBase64URL(asn.signature),
      userHandle: asn.userHandle ? bufferToBase64URL(asn.userHandle) : undefined,
    },
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    authenticatorAttachment: (cred as any).authenticatorAttachment,
  };
}
