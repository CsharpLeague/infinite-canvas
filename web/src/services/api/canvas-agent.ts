import { aiApiUrl, aiHeaders, refreshRemoteUser } from "@/services/api/image";
import type { AiConfig } from "@/stores/use-config-store";
import type { CanvasAgentProtocolMessage, CanvasAgentToolCall } from "@/app/(user)/canvas/types";
import type { CanvasAgentToolDefinition } from "@/app/(user)/canvas/agent/canvas-agent-tools";

export type CanvasAgentModelTurn = {
    content: string;
    toolCalls: CanvasAgentToolCall[];
    usedJsonFallback: boolean;
};

type RequestCanvasAgentTurnInput = {
    config: AiConfig;
    systemPrompt: string;
    messages: CanvasAgentProtocolMessage[];
    tools: CanvasAgentToolDefinition[];
    allowTools: boolean;
    onTextDelta?: (text: string) => void;
    signal?: AbortSignal;
};

type ChatCompletionPayload = {
    code?: number;
    msg?: string;
    error?: { message?: string };
    choices?: Array<{
        message?: {
            content?: string | null;
            tool_calls?: Array<{
                id?: string;
                function?: { name?: string; arguments?: string | Record<string, unknown> };
            }>;
        };
    }>;
    data?: {
        output_text?: string;
        output?: ResponsesOutputItem[];
        choices?: Array<{
            message?: {
                content?: string | null;
                tool_calls?: Array<{
                    id?: string;
                    function?: { name?: string; arguments?: string | Record<string, unknown> };
                }>;
            };
        }>;
    };
    output_text?: string;
    output?: ResponsesOutputItem[];
};

type ResponsesOutputItem = {
    type?: string;
    id?: string;
    call_id?: string;
    name?: string;
    arguments?: string | Record<string, unknown>;
    content?: Array<{ type?: string; text?: string }>;
};

type ChatCompletionChunk = {
    choices?: Array<{
        delta?: {
            content?: string;
            tool_calls?: Array<{
                index?: number;
                id?: string;
                function?: { name?: string; arguments?: string };
            }>;
        };
    }>;
    type?: string;
    delta?: string;
    item?: ResponsesOutputItem;
    response?: ChatCompletionPayload;
    error?: { message?: string };
    rate_limits?: { allowed?: boolean; limit_reached?: boolean; reset_after_seconds?: number };
};

class CanvasAgentRequestError extends Error {
    status: number;

    constructor(message: string, status: number) {
        super(message);
        this.name = "CanvasAgentRequestError";
        this.status = status;
    }
}

export async function requestCanvasAgentTurn(input: RequestCanvasAgentTurnInput): Promise<CanvasAgentModelTurn> {
    const requestConfig = {
        ...input.config,
        model: input.config.textModel || input.config.model,
        activeChannelId: input.config.textChannelId || input.config.activeChannelId,
        textChannelId: input.config.textChannelId,
    };
    const configuredSystemPrompt = (requestConfig.systemPrompts.text || requestConfig.systemPrompt).trim();
    const systemPrompt = configuredSystemPrompt ? configuredSystemPrompt + "\n\n" + input.systemPrompt : input.systemPrompt;
    let messages = input.messages;
    let tools = input.allowTools ? input.tools : [];
    let usedJsonFallback = !input.allowTools;
    let requestError: unknown;

    for (let attempt = 0; attempt < 3; attempt++) {
        try {
            const message = await requestCompletion(requestConfig, systemPrompt, messages, tools, input.signal, input.onTextDelta);
            return { ...message, usedJsonFallback };
        } catch (error) {
            requestError = error;
            if (hasImageContent(messages) && isImageCompatibilityError(error)) {
                messages = stripImageContent(messages);
                continue;
            }
            if (tools.length && isToolCompatibilityError(error)) {
                tools = [];
                usedJsonFallback = true;
                continue;
            }
            throw error;
        }
    }
    throw requestError;
}

async function requestCompletion(config: AiConfig, systemPrompt: string, messages: CanvasAgentProtocolMessage[], tools: CanvasAgentToolDefinition[], signal?: AbortSignal, onTextDelta?: (text: string) => void) {
    const body: Record<string, unknown> = {
        model: config.model,
        messages: [{ role: "system", content: systemPrompt }, ...messages.map(toRequestMessage)],
        stream: true,
    };
    if (tools.length) {
        body.tools = tools;
        body.tool_choice = "auto";
    }

    const response = await fetch(aiApiUrl(config, "/chat/completions"), {
        method: "POST",
        headers: aiHeaders(config, "application/json"),
        body: JSON.stringify(body),
        signal,
    });
    const payload = response.ok && response.headers.get("Content-Type")?.toLowerCase().includes("text/event-stream")
        ? await readCompletionStream(response, onTextDelta)
        : (await response.json().catch(() => ({}))) as ChatCompletionPayload;
    if (!response.ok || (typeof payload.code === "number" && payload.code !== 0)) {
        throw new CanvasAgentRequestError(readError(payload, response.status), response.status);
    }
    const message = readResponseMessage(payload);
    if (!message) throw new CanvasAgentRequestError(readError(payload, response.status) || "文本模型没有返回内容", response.status);

    refreshRemoteUser(config);
    return {
        content: typeof message.content === "string" ? message.content : "",
        toolCalls: (message.tool_calls || []).flatMap((toolCall, index) => {
            const name = toolCall.function?.name?.trim();
            if (!name) return [];
            return [
                {
                    id: toolCall.id || "tool-call-" + index,
                    name,
                    arguments: parseToolArguments(toolCall.function?.arguments),
                },
            ];
        }),
    };
}

function readResponseMessage(payload: ChatCompletionPayload) {
    const chatMessage = payload.choices?.[0]?.message || payload.data?.choices?.[0]?.message;
    if (chatMessage) return chatMessage;

    const output = payload.output || payload.data?.output || [];
    const content = payload.output_text || payload.data?.output_text || output
        .flatMap((item) => item.content || [])
        .filter((item) => item.type === "output_text" || item.type === "text")
        .map((item) => item.text || "")
        .join("");
    const toolCalls = output.flatMap((item) => {
        if (item.type !== "function_call" || !item.name) return [];
        return [{
            id: item.call_id || item.id,
            function: { name: item.name, arguments: item.arguments },
        }];
    });
    if (!content && !toolCalls.length) return undefined;
    return { content, tool_calls: toolCalls };
}

function toRequestMessage(message: CanvasAgentProtocolMessage) {
    if (message.role === "assistant") {
        return {
            role: "assistant",
            content: message.content || null,
            ...(message.toolCalls?.length
                ? {
                      tool_calls: message.toolCalls.map((toolCall) => ({
                          id: toolCall.id,
                          type: "function",
                          function: { name: toolCall.name, arguments: JSON.stringify(toolCall.arguments) },
                      })),
                  }
                : {}),
        };
    }
    if (message.role === "tool") {
        return {
            role: "tool",
            content: message.content,
            tool_call_id: message.toolCallId,
            name: message.name,
        };
    }
    return { role: message.role, content: message.content };
}

function parseToolArguments(value: string | Record<string, unknown> | undefined) {
    if (!value) return {};
    if (typeof value === "object") return value;
    try {
        const parsed = JSON.parse(value);
        return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Record<string, unknown>) : {};
    } catch {
        return {};
    }
}

function readError(payload: ChatCompletionPayload, status: number) {
    const message = payload.error?.message || payload.msg || "";
    if (status === 524 || /(?:error code|status)\s*:?\s*524|\b524\b/i.test(message)) {
        return "上游模型响应超时（524），已完成的画布操作不会丢失，可以继续发送消息。";
    }
    return message || (status ? "文本模型请求失败：" + status : "文本模型请求失败");
}

async function readCompletionStream(response: Response, onTextDelta?: (text: string) => void): Promise<ChatCompletionPayload> {
    if (!response.body) throw new CanvasAgentRequestError("文本模型没有返回可读取的流式响应", response.status);
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const toolCalls = new Map<number, { id?: string; name?: string; arguments: string }>();
    let buffer = "";
    let content = "";
    let completed: ChatCompletionPayload | undefined;

    const consume = (block: string) => {
        const data = block.split(/\r?\n/).filter((line) => line.startsWith("data:")).map((line) => line.slice(5).trimStart()).join("\n").trim();
        if (!data || data === "[DONE]") return;
        const event = JSON.parse(data) as ChatCompletionChunk;
        if (event.error?.message) throw new CanvasAgentRequestError(event.error.message, response.status);
        if (event.type === "codex.rate_limits" && (event.rate_limits?.allowed === false || event.rate_limits?.limit_reached)) {
            const seconds = event.rate_limits.reset_after_seconds;
            const reset = typeof seconds === "number" ? `，预计 ${formatResetDuration(seconds)} 后恢复` : "";
            throw new CanvasAgentRequestError("当前模型渠道额度已用完" + reset + "，请更换文本模型或渠道。", 429);
        }
        if (event.type === "response.completed" && event.response) completed = event.response;
        if (event.type === "response.output_text.delta" && event.delta) {
            content += event.delta;
            onTextDelta?.(content);
        }
        if (event.type === "response.output_item.done" && event.item?.type === "function_call" && event.item.name) {
            toolCalls.set(toolCalls.size, {
                id: event.item.call_id || event.item.id,
                name: event.item.name,
                arguments: typeof event.item.arguments === "string" ? event.item.arguments : JSON.stringify(event.item.arguments || {}),
            });
        }
        for (const choice of event.choices || []) {
            if (choice.delta?.content) {
                content += choice.delta.content;
                onTextDelta?.(content);
            }
            for (const call of choice.delta?.tool_calls || []) {
                const index = call.index || 0;
                const current = toolCalls.get(index) || { arguments: "" };
                if (call.id) current.id = call.id;
                if (call.function?.name) current.name = call.function.name;
                if (call.function?.arguments) current.arguments += call.function.arguments;
                toolCalls.set(index, current);
            }
        }
    };

    while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        let separator = buffer.match(/\r?\n\r?\n/);
        while (separator?.index !== undefined) {
            consume(buffer.slice(0, separator.index));
            buffer = buffer.slice(separator.index + separator[0].length);
            separator = buffer.match(/\r?\n\r?\n/);
        }
    }
    buffer += decoder.decode();
    if (buffer.trim()) consume(buffer);
    if (completed) return completed;
    return {
        choices: [{
            message: {
                content,
                tool_calls: [...toolCalls.values()].filter((call) => call.name).map((call) => ({ id: call.id, function: { name: call.name, arguments: call.arguments } })),
            },
        }],
    };
}

function formatResetDuration(seconds: number) {
    if (seconds < 60) return Math.max(1, Math.ceil(seconds)) + " 秒";
    if (seconds < 3600) return Math.ceil(seconds / 60) + " 分钟";
    if (seconds < 86400) return Math.ceil(seconds / 3600) + " 小时";
    return Math.ceil(seconds / 86400) + " 天";
}

function hasImageContent(messages: CanvasAgentProtocolMessage[]) {
    return messages.some((message) => (message.role === "user" || message.role === "system") && Array.isArray(message.content) && message.content.some((item) => item.type === "image_url"));
}

function stripImageContent(messages: CanvasAgentProtocolMessage[]) {
    return messages.map((message): CanvasAgentProtocolMessage => {
        if ((message.role === "user" || message.role === "system") && Array.isArray(message.content)) {
            return { role: message.role, content: message.content.filter((item) => item.type === "text") };
        }
        return message;
    });
}

function isImageCompatibilityError(error: unknown) {
    return error instanceof CanvasAgentRequestError && /image_url|image input|vision|multimodal|content.*array|unsupported.*image|不支持.*图片|图像输入/i.test(error.message);
}

function isToolCompatibilityError(error: unknown) {
    if (!(error instanceof CanvasAgentRequestError)) return false;
    return error.status === 400 || error.status === 422 || /tools?|tool_choice|function.?call|unknown field|unsupported|not support|不支持|未知字段/i.test(error.message);
}
