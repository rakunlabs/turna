// Zero-dependency WebAuthn registration helpers for the auth UI.
// Wire format uses URL-safe base64 without padding, matching the server.

export function bufferToBase64URL(buf: ArrayBuffer | Uint8Array): string {
  const bytes = buf instanceof Uint8Array ? buf : new Uint8Array(buf);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function base64URLToBuffer(s: string): Uint8Array<ArrayBuffer> {
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

export interface ServerCreationOptions {
  challenge: string;
  rp: { id: string; name: string };
  user: { id: string; name: string; displayName: string };
  pubKeyCredParams: Array<{ type: "public-key"; alg: number }>;
  timeout?: number;
  excludeCredentials?: Array<{ type: "public-key"; id: string; transports?: string[] }>;
  authenticatorSelection?: AuthenticatorSelectionCriteria;
  attestation?: AttestationConveyancePreference;
}

export interface RegistrationResponseJSON {
  id: string;
  rawId: string;
  type: "public-key";
  response: {
    clientDataJSON: string;
    attestationObject: string;
    transports?: string[];
  };
  authenticatorAttachment?: string;
}

/** Run the registration (attestation) ceremony in the browser. */
export async function startRegistration(opts: ServerCreationOptions): Promise<RegistrationResponseJSON> {
  if (!isWebAuthnSupported()) throw new Error("webauthn not supported");

  const publicKey: PublicKeyCredentialCreationOptions = {
    challenge: base64URLToBuffer(opts.challenge),
    rp: opts.rp,
    user: {
      id: base64URLToBuffer(opts.user.id),
      name: opts.user.name,
      displayName: opts.user.displayName,
    },
    pubKeyCredParams: opts.pubKeyCredParams,
    timeout: opts.timeout,
    excludeCredentials: (opts.excludeCredentials ?? []).map((c) => ({
      type: c.type,
      id: base64URLToBuffer(c.id),
      transports: c.transports as AuthenticatorTransport[] | undefined,
    })),
    authenticatorSelection: opts.authenticatorSelection,
    attestation: opts.attestation,
  };

  const cred = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
  if (!cred) throw new Error("registration was cancelled");

  const att = cred.response as AuthenticatorAttestationResponse;
  return {
    id: cred.id,
    rawId: bufferToBase64URL(cred.rawId),
    type: "public-key",
    response: {
      clientDataJSON: bufferToBase64URL(att.clientDataJSON),
      attestationObject: bufferToBase64URL(att.attestationObject),
      transports: typeof att.getTransports === "function" ? att.getTransports() : undefined,
    },
    authenticatorAttachment: (cred as unknown as { authenticatorAttachment?: string }).authenticatorAttachment,
  };
}
