// End-to-end MCP client: discovers turna as the authorization server from
// the 401 challenge (RFC 9728), registers dynamically (RFC 7591), runs the
// PKCE authorization code flow and calls tools. The "browser" step signs in
// on the turna login page with a password, like a user would.
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
import { UnauthorizedError } from "@modelcontextprotocol/sdk/client/auth.js";

const base = process.env.TURNA ?? "http://localhost:8082";
const mcpURL = new URL(`${base}/mcp`);
const redirectUrl = "http://127.0.0.1:33418/callback";
const [username, password] = [process.env.MCP_USER ?? "demo", process.env.MCP_PASS ?? "demo-password-123"];

const store = {};
let pendingCode;

const provider = {
  get redirectUrl() {
    return redirectUrl;
  },
  get clientMetadata() {
    return {
      client_name: "turna MCP demo",
      redirect_uris: [redirectUrl],
      grant_types: ["authorization_code", "refresh_token"],
      response_types: ["code"],
      token_endpoint_auth_method: "none",
    };
  },
  clientInformation: () => store.client,
  saveClientInformation: (info) => (store.client = info),
  tokens: () => store.tokens,
  saveTokens: (tokens) => (store.tokens = tokens),
  saveCodeVerifier: (v) => (store.verifier = v),
  codeVerifier: () => store.verifier,
  // the "browser": sign in on the turna login page, then follow the
  // authorize → consent → callback redirects
  async redirectToAuthorization(url) {
    console.log("authorize:", url.origin + url.pathname);
    const cookies = new Map();
    const jar = () => [...cookies].map(([k, v]) => `${k}=${v}`).join("; ");
    const keep = (res) => {
      for (const c of res.headers.getSetCookie()) {
        const [pair] = c.split(";");
        const [k, v] = pair.split("=");
        cookies.set(k, v);
      }
    };

    let target = url.toString();
    for (let i = 0; i < 10; i++) {
      const res = await fetch(target, { redirect: "manual", headers: { cookie: jar(), accept: "text/html" } });
      keep(res);
      const location = res.headers.get("location");
      if (!location) {
        // consent page: approve
        const html = await res.text();
        const flow = html.match(/name="flow" value="([^"]+)"/)?.[1];
        const token = html.match(/name="consent_token" value="([^"]+)"/)?.[1];
        if (!flow) throw new Error(`unexpected page ${res.status}`);
        const form = new URLSearchParams({ flow, consent_token: token, action: "approve" });
        const post = await fetch(`${base}/auth/oauth2/consent`, {
          method: "POST",
          redirect: "manual",
          headers: { cookie: jar(), "content-type": "application/x-www-form-urlencoded" },
          body: form,
        });
        target = post.headers.get("location");
        if (target?.startsWith(redirectUrl)) {
          pendingCode = new URL(target).searchParams.get("code");
          return;
        }
        if (!target) throw new Error(`consent answered ${post.status}: ${(await post.text()).match(/<p>([^<]*)<\/p>/)?.[1] ?? ""}`);
        continue;
      }

      const next = new URL(location, target);
      if (next.toString().startsWith(redirectUrl)) {
        pendingCode = next.searchParams.get("code");
        return;
      }
      if (next.pathname.startsWith("/login/")) {
        const login = await fetch(`${base}/login/auth/token/turna`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ username, password }),
        });
        keep(login);
        if (!login.ok) throw new Error(`login failed ${login.status}`);
        target = `${base}${next.searchParams.get("redirect_path")}`;
        continue;
      }
      target = next.toString();
    }
    throw new Error("too many redirects");
  },
};

async function connect() {
  const transport = new StreamableHTTPClientTransport(mcpURL, { authProvider: provider });
  const client = new Client({ name: "turna-demo-client", version: "1.0.0" });
  try {
    await client.connect(transport);
  } catch (err) {
    if (!(err instanceof UnauthorizedError) || !pendingCode) throw err;
    await transport.finishAuth(pendingCode);
    return connect();
  }

  return client;
}

const client = await connect();
console.log("registered client:", store.client.client_id);
console.log("tools:", (await client.listTools()).tools.map((t) => t.name).join(", "));
console.log((await client.callTool({ name: "whoami", arguments: {} })).content[0].text);
console.log("2 + 3 =", (await client.callTool({ name: "add", arguments: { a: 2, b: 3 } })).content[0].text);
await client.close();
