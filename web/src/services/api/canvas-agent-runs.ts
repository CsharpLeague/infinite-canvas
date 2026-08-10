import type { AiConfig } from "@/stores/use-config-store";
import { apiGet, apiPost, apiPostWithHeaders } from "@/services/api/request";

export type CanvasAgentRun = {
    id: string;
    token: string;
    sessionId: string;
    skillId: string;
    status: "running" | "waiting_tool" | "completed" | "failed" | "cancelled";
    phase: string;
    pendingToolCalls?: CanvasAgentRunToolCall[];
};

export type CanvasAgentRunToolCall = { id: string; name: string; arguments: Record<string, unknown> };
export type CanvasAgentRunEvent = { id: number; type: string; data: Record<string, unknown> };

export function createCanvasAgentRun(input: {
    token?: string;
    sessionId: string;
    canvasId?: string;
    skillId?: string;
    phase: string;
    config: AiConfig;
    message: string;
    canvasContext: unknown;
}) {
    const channelId = input.config.textChannelId || input.config.activeChannelId;
    return apiPost<CanvasAgentRun>("/api/canvas-agent/runs", {
        sessionId: input.sessionId,
        canvasId: input.canvasId,
        skillId: input.skillId,
        phase: input.phase,
        model: input.config.textModel || input.config.model,
        ...(input.config.channelMode === "local" ? { userChannelId: channelId } : { channelId }),
        message: input.message,
        canvasContext: input.canvasContext,
    }, input.token);
}

export function submitCanvasAgentToolResults(run: CanvasAgentRun, results: Array<{ callId: string; name: string; result: unknown }>, token?: string) {
    return apiPostWithHeaders<CanvasAgentRun>(`/api/canvas-agent/runs/${encodeURIComponent(run.id)}/tool-results`, { results }, runHeaders(run, token));
}

export function recoverCanvasAgentRun(sessionId: string, canvasId: string | undefined, token?: string) {
    return apiGet<CanvasAgentRun>("/api/canvas-agent/runs/active", { sessionId, canvasId }, token);
}

export function cancelCanvasAgentRun(run: CanvasAgentRun, token?: string) {
    return apiPostWithHeaders<CanvasAgentRun>(`/api/canvas-agent/runs/${encodeURIComponent(run.id)}/cancel`, {}, runHeaders(run, token));
}

export async function streamCanvasAgentRun(run: CanvasAgentRun, options: { after?: number; token?: string; signal?: AbortSignal; onCursor?: (id: number) => void; onEvent: (event: CanvasAgentRunEvent) => void }) {
    const response = await fetch(`/api/canvas-agent/runs/${encodeURIComponent(run.id)}/events?after=${options.after || 0}`, {
        headers: { "X-Agent-Run-Token": run.token, ...(options.token ? { Authorization: `Bearer ${options.token}` } : {}) },
        signal: options.signal,
    });
    if (!response.ok || !response.body) throw new Error(await readStreamError(response));
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    let lastEventId = options.after || 0;
    while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let match = buffer.match(/\r?\n\r?\n/);
        while (match?.index !== undefined) {
            const block = buffer.slice(0, match.index);
            buffer = buffer.slice(match.index + match[0].length);
            const event = parseEvent(block);
            if (event) {
                lastEventId = event.id || lastEventId;
                options.onCursor?.(lastEventId);
                options.onEvent(event);
            }
            match = buffer.match(/\r?\n\r?\n/);
        }
    }
    return lastEventId;
}

function parseEvent(block: string): CanvasAgentRunEvent | undefined {
    if (!block || block.startsWith(":")) return;
    let id = 0;
    let type = "message";
    const data: string[] = [];
    for (const line of block.split(/\r?\n/)) {
        if (line.startsWith("id:")) id = Number(line.slice(3).trim()) || 0;
        else if (line.startsWith("event:")) type = line.slice(6).trim();
        else if (line.startsWith("data:")) data.push(line.slice(5).trimStart());
    }
    if (!data.length) return;
    const parsed = JSON.parse(data.join("\n")) as Record<string, unknown>;
    if (type === "error") throw new Error(typeof parsed.message === "string" ? parsed.message : "Agent 事件流失败");
    return { id, type, data: parsed };
}

async function readStreamError(response: Response) {
    const payload = await response.json().catch(() => undefined) as { msg?: string } | undefined;
    return payload?.msg || `Agent 事件流失败：${response.status}`;
}

function runHeaders(run: CanvasAgentRun, token?: string) {
    return { "X-Agent-Run-Token": run.token, ...(token ? { Authorization: `Bearer ${token}` } : {}) };
}
