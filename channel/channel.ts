#!/usr/bin/env bun
//
// Belayer MCP Channel Server
//
// A thin adapter that bridges:
//   INBOUND:  HTTP POST → Claude Code channel events (pipeline notifications)
//   OUTBOUND: MCP tool calls → HTTP POST to worker (submit, status)
//
// All routing logic, state management, and business logic stays in the Go worker.
// This is just a bridge.
//
// Environment variables:
//   BELAYER_CHANNEL_PORT — HTTP listen port for inbound events (default: 8790)
//   BELAYER_WORKER_PORT  — Worker HTTP API port for outbound tool calls (default: 8780)
//
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import {
  ListToolsRequestSchema,
  CallToolRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";

const CHANNEL_PORT = parseInt(process.env.BELAYER_CHANNEL_PORT || "8790");
const WORKER_PORT = parseInt(process.env.BELAYER_WORKER_PORT || "8780");

const mcp = new Server(
  { name: "belayer-channel", version: "0.0.2" },
  {
    capabilities: {
      experimental: { "claude/channel": {} },
      tools: {}, // Enable tool discovery
    },
    instructions: `You are connected to a belayer pipeline via the belayer-channel.

Pipeline events arrive as <channel source="belayer-channel" event="..." pipeline_id="..."> tags.
Events include: pipeline_started, phase_started, role_completed, dependency_ready, risk_gate, flare, permission_needed, pipeline_completed.

When you receive events:
- Report status updates clearly, grouped by pipeline_id when multiple are running
- If a flare arrives, alert the user immediately
- If a risk_gate arrives, explain and help the user decide

You have tools available:
- submit: Start a pipeline run with a spec. Use when the user says "send it", "submit", or "start the pipeline".
- status: Check active pipeline runs. Use when the user asks "what's running?"`,
  }
);

// --- MCP Tools: submit + status ---

mcp.setRequestHandler(ListToolsRequestSchema, async () => ({
  tools: [
    {
      name: "submit",
      description:
        "Submit a spec to start a pipeline run. The spec is sent to the decomposer which breaks it into per-repo tasks.",
      inputSchema: {
        type: "object" as const,
        properties: {
          spec: {
            type: "string",
            description:
              "The implementation spec/PRD to execute. Should be detailed enough for an agent to implement.",
          },
          repos: {
            type: "array",
            items: { type: "string" },
            description:
              "Optional: specific repo names to target. If omitted, uses pipeline default.",
          },
          pipeline: {
            type: "string",
            description:
              "Optional: pipeline YAML file to use. Defaults to belayer-pipeline.yaml.",
          },
        },
        required: ["spec"],
      },
    },
    {
      name: "status",
      description: "Check the status of active pipeline runs.",
      inputSchema: {
        type: "object" as const,
        properties: {},
      },
    },
  ],
}));

mcp.setRequestHandler(CallToolRequestSchema, async (req) => {
  const { name } = req.params;
  const args = req.params.arguments as Record<string, unknown>;

  if (name === "submit") {
    try {
      const resp = await fetch(`http://127.0.0.1:${WORKER_PORT}/start`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          spec: args.spec,
          repos: args.repos || [],
          source: "interactive",
          external_id: `submit-${Date.now()}`,
          pipeline_name: (args.pipeline as string) || "",
        }),
      });

      if (!resp.ok) {
        const errText = await resp.text();
        return {
          content: [
            {
              type: "text" as const,
              text: `Failed to start pipeline: ${resp.status} ${errText}`,
            },
          ],
        };
      }

      const result = (await resp.json()) as {
        workflow_id?: string;
        pipeline_name?: string;
      };
      return {
        content: [
          {
            type: "text" as const,
            text: `Pipeline started!\n  Workflow: ${result.workflow_id}\n  Pipeline: ${result.pipeline_name}\n\nEvents will stream in as the pipeline progresses.`,
          },
        ],
      };
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      return {
        content: [
          {
            type: "text" as const,
            text: `Cannot reach worker. Is 'belayer worker' running?\n\nError: ${msg}`,
          },
        ],
      };
    }
  }

  if (name === "status") {
    try {
      const resp = await fetch(`http://127.0.0.1:${WORKER_PORT}/status`);
      if (!resp.ok) {
        return {
          content: [
            {
              type: "text" as const,
              text: `Worker returned ${resp.status}`,
            },
          ],
        };
      }
      const result = await resp.json();
      return {
        content: [
          { type: "text" as const, text: JSON.stringify(result, null, 2) },
        ],
      };
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      return {
        content: [
          {
            type: "text" as const,
            text: `Cannot reach worker. Is 'belayer worker' running?\n\nError: ${msg}`,
          },
        ],
      };
    }
  }

  return {
    content: [
      { type: "text" as const, text: `Unknown tool: ${name}` },
    ],
  };
});

// --- Connect to Claude Code ---
await mcp.connect(new StdioServerTransport());

// --- HTTP listener: Temporal worker POSTs events here ---
Bun.serve({
  port: CHANNEL_PORT,
  hostname: "127.0.0.1",
  async fetch(req: Request): Promise<Response> {
    if (req.method !== "POST") {
      return new Response("Method not allowed", { status: 405 });
    }

    try {
      const body = (await req.json()) as {
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

console.error(
  `belayer-channel: listening on http://127.0.0.1:${CHANNEL_PORT} (events), worker at :${WORKER_PORT}`
);
