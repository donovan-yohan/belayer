#!/usr/bin/env bun
//
// Belayer MCP Channel Server
//
// A thin adapter that bridges HTTP → Claude Code channel events.
// The Temporal worker POSTs pipeline events to this server's HTTP endpoint,
// and they arrive in the Claude Code session as <channel source="belayer-channel"> tags.
//
// All routing logic, state management, and business logic stays in the Go worker.
// This is just a bridge: HTTP in → MCP notification out.
//
// Environment variables:
//   BELAYER_CHANNEL_PORT — HTTP listen port (default: 8790)
//
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

const PORT = parseInt(process.env.BELAYER_CHANNEL_PORT || "8790");

const mcp = new Server(
  { name: "belayer-channel", version: "0.0.1" },
  {
    capabilities: {
      experimental: { "claude/channel": {} },
    },
    instructions: `You are connected to a belayer pipeline via the belayer-channel.

Pipeline events arrive as <channel source="belayer-channel" event="..."> tags. These events include:
- pipeline_started: A new pipeline run has begun
- phase_started: A pipeline phase (approach/ascent/send) has started
- role_completed: A role in the pipeline has finished its work
- dependency_ready: A repo dependency has been met, you can now proceed
- risk_gate: A risk threshold was exceeded, human review needed
- flare: A session needs help
- permission_needed: A session is stuck waiting for permission approval
- pipeline_completed: The entire pipeline has finished

When you receive these events:
- Report status updates to the user clearly
- If a flare arrives, alert the user immediately and explain what help is needed
- If a risk_gate arrives, explain the decision and help the user approve or override
- If permission_needed arrives, let the user know which session needs approval`,
  }
);

// Connect to Claude Code over stdio
await mcp.connect(new StdioServerTransport());

// HTTP listener: Temporal worker POSTs events here
Bun.serve({
  port: PORT,
  hostname: "127.0.0.1",
  async fetch(req: Request): Promise<Response> {
    if (req.method !== "POST") {
      return new Response("Method not allowed", { status: 405 });
    }

    try {
      const body = await req.json() as {
        event?: string;
        content?: string;
        meta?: Record<string, string>;
      };

      const meta: Record<string, string> = {
        ...(body.meta || {}),
      };
      if (body.event) {
        meta.event = body.event;
      }

      await mcp.notification({
        method: "notifications/claude/channel",
        params: {
          content: body.content || JSON.stringify(body),
          meta,
        },
      });

      return new Response("ok");
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      console.error(`belayer-channel: error processing event: ${message}`);
      return new Response("error", { status: 500 });
    }
  },
});

console.error(`belayer-channel: listening on http://127.0.0.1:${PORT}`);
