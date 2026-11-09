// A minimal MCP server (Streamable HTTP) that trusts the identity turna puts
// in X-User after validating the OAuth access token.
import http from "node:http";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StreamableHTTPServerTransport } from "@modelcontextprotocol/sdk/server/streamableHttp.js";
import { z } from "zod";

const port = Number(process.env.PORT ?? 9090);

http
  .createServer(async (req, res) => {
    const user = req.headers["x-user"] ?? "anonymous";
    const scope = req.headers["x-scope"] ?? "";

    const server = new McpServer({ name: "turna-demo", version: "1.0.0" });
    server.registerTool(
      "whoami",
      { description: "Returns the signed-in user", inputSchema: {} },
      async () => ({ content: [{ type: "text", text: `you are ${user} (scope: ${scope})` }] }),
    );
    server.registerTool(
      "add",
      { description: "Adds two numbers", inputSchema: { a: z.number(), b: z.number() } },
      async ({ a, b }) => ({ content: [{ type: "text", text: String(a + b) }] }),
    );

    // stateless: one transport per request
    const transport = new StreamableHTTPServerTransport({ sessionIdGenerator: undefined });
    res.on("close", () => {
      transport.close();
      server.close();
    });

    let body = "";
    for await (const chunk of req) body += chunk;

    await server.connect(transport);
    await transport.handleRequest(req, res, body ? JSON.parse(body) : undefined);
  })
  .listen(port, () => console.log(`mcp server on :${port}`));
