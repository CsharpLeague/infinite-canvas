import { requestCanvasAgentTurn } from "@/services/api/canvas-agent";
import { cancelCanvasAgentRun, createCanvasAgentRun, recoverCanvasAgentRun, streamCanvasAgentRun, submitCanvasAgentToolResults, type CanvasAgentRun, type CanvasAgentRunToolCall } from "@/services/api/canvas-agent-runs";
import { useUserStore } from "@/stores/use-user-store";
import type { AiConfig } from "@/stores/use-config-store";
import type {
    CanvasAgentContent,
    CanvasAgentProtocolMessage,
    CanvasAgentState,
    CanvasAssistantMode,
    CanvasAssistantMessageStatus,
    CanvasAssistantReference,
} from "../types";
import type { CanvasAgentContext } from "./canvas-agent-context";
import { buildCanvasAgentSkillPrompt, buildCanvasChatPrompt } from "./canvas-agent-skills";
import {
    CANVAS_AGENT_TOOLS,
    canvasAgentActionLabel,
    normalizeCanvasAgentAction,
    parseCanvasAgentJson,
    userLikelyRequestedCanvasAction,
    type CanvasAgentAction,
    type CanvasAgentToolResult,
} from "./canvas-agent-tools";

const MAX_AGENT_STEPS = 12;
const MAX_PROTOCOL_MESSAGES = 120;
const MAX_PROTOCOL_CHARACTERS = 32_000;

function trimProtocolMessages(messages: CanvasAgentProtocolMessage[]) {
    const trimmed: CanvasAgentProtocolMessage[] = [];
    let characters = 0;
    for (const message of messages.slice(-MAX_PROTOCOL_MESSAGES).reverse()) {
        const size = JSON.stringify(message).length;
        if (trimmed.length && characters + size > MAX_PROTOCOL_CHARACTERS) break;
        trimmed.unshift(message);
        characters += size;
    }
    while (trimmed[0]?.role === "tool") trimmed.shift();
    return trimmed;
}

export type CanvasAgentRuntimeEvent = {
    status: CanvasAssistantMessageStatus;
    label: string;
};

export type RunCanvasAgentInput = {
    sessionId: string;
    skillId?: string;
    existingRun?: CanvasAgentRun;
    toolResults?: Record<string, CanvasAgentToolResult>;
    toolExecutions?: Record<string, { status: "started" | "completed"; result?: CanvasAgentToolResult }>;
    onRunChange?: (run?: CanvasAgentRun) => void;
    onToolResult?: (callId: string, result: CanvasAgentToolResult) => void;
    onToolStart?: (callId: string) => void;
    mode: CanvasAssistantMode;
    config: AiConfig;
    initialState: CanvasAgentState;
    protocolMessages: CanvasAgentProtocolMessage[];
    userText: string;
    references: CanvasAssistantReference[];
    getContext: (state: CanvasAgentState) => CanvasAgentContext;
    executeAction: (action: CanvasAgentAction) => Promise<CanvasAgentToolResult>;
    onEvent?: (event: CanvasAgentRuntimeEvent) => void;
    onTextDelta?: (text: string) => void;
    onCheckpoint?: (checkpoint: { state: CanvasAgentState; protocolMessages: CanvasAgentProtocolMessage[] }) => void;
    signal?: AbortSignal;
};

export type RunCanvasAgentResult = {
    reply: string;
    state: CanvasAgentState;
    protocolMessages: CanvasAgentProtocolMessage[];
};

export function createCanvasAgentState(): CanvasAgentState {
    return {
        phase: "intake",
        approvedNodeIds: [],
        referenceNodeIds: [],
        pendingTaskIds: [],
        completedTaskIds: [],
    };
}

export async function runCanvasAgent(input: RunCanvasAgentInput): Promise<RunCanvasAgentResult> {
    if (input.mode === "agent") return runServerCanvasAgent(input);
    let state = { ...createCanvasAgentState(), ...(input.initialState || {}) };
    let allowTools = input.mode === "agent";
    let hasExecutedActions = false;
    let protocolMessages: CanvasAgentProtocolMessage[] = trimProtocolMessages([
        ...(Array.isArray(input.protocolMessages) ? input.protocolMessages : []),
        { role: "user" as const, content: buildUserContent(input.userText, input.references, input.config.textModel || input.config.model) },
    ]);

    for (let step = 0; step < MAX_AGENT_STEPS; step++) {
        throwIfAborted(input.signal);
        input.onEvent?.({ status: "thinking", label: input.mode === "chat" ? "正在思考" : step ? "正在根据画布结果继续" : "正在理解画布和创作目标" });
        const context = input.mode === "agent" ? input.getContext(state) : undefined;
        let turn;
        try {
            turn = await requestCanvasAgentTurn({
                config: input.config,
                systemPrompt: input.mode === "chat" ? buildCanvasChatPrompt() : buildCanvasAgentSkillPrompt(state.phase, input.userText, context!),
                messages: protocolMessages,
                tools: CANVAS_AGENT_TOOLS,
                allowTools,
                onTextDelta: input.onTextDelta,
                signal: input.signal,
            });
        } catch (error) {
            if (hasExecutedActions && /524|响应超时/.test(error instanceof Error ? error.message : String(error))) {
                const reply = "上游模型在整理结果时响应超时，但本轮已经完成的画布操作均已保存，不需要重新执行。你可以直接继续发送下一步要求。";
                protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: reply }]);
                return { reply, state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) };
            }
            throw error;
        }
        if (input.mode === "chat") {
            const reply = turn.content.trim() || "你可以继续补充想讨论的内容。";
            protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: reply }]);
            return { reply, state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) };
        }
        if (turn.usedJsonFallback) allowTools = false;

        const parsedJson = parseCanvasAgentJson(turn.content);
        const nativeActions = turn.toolCalls.map((toolCall) => normalizeCanvasAgentAction(toolCall.name, toolCall.arguments, toolCall.id));
        const arrangeRequested = /整理|排列|排序|对齐|布局|排版|重新摆放/.test(input.userText) && !/(不要|别|无需|不用).{0,8}(整理|排列|排序|对齐|布局|排版|重新摆放)/.test(input.userText);
        const actions = (nativeActions.length ? nativeActions : parsedJson.actions).filter((action) => action.name !== "arrange_nodes" || arrangeRequested);

        if (!actions.length) {
            const reply = (parsedJson.parsed ? parsedJson.reply : turn.content).trim();
            if (!hasExecutedActions && userLikelyRequestedCanvasAction(input.userText) && !looksLikeClarifyingQuestion(reply)) {
                const unsupported = "当前文本模型没有返回可执行的画布工具指令。可以继续讨论文本内容，但无法可靠地自动创建节点或执行生成；请在全局配置中更换支持 Tool Calling 或稳定 JSON 输出的文本模型。";
                protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: unsupported }]);
                return { reply: unsupported, state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) };
            }
            const finalReply = reply || "我已经读取当前画布。请告诉我下一步要继续完善哪一部分。";
            protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: finalReply }]);
            return { reply: finalReply, state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) };
        }

        input.onEvent?.({ status: "running", label: actions.length === 1 ? canvasAgentActionLabel(actions[0]) : "正在执行 " + actions.length + " 个画布操作" });
        const assistantToolMessage: CanvasAgentProtocolMessage = nativeActions.length
            ? { role: "assistant", content: turn.content || undefined, toolCalls: actions.map((action) => ({ id: action.id, name: action.name, arguments: action.arguments })) }
            : { role: "assistant", content: turn.content };

        const results = await executeActions(actions, state, input.executeAction, input.signal, input.onEvent);
        hasExecutedActions = true;
        state = results.state;

        if (nativeActions.length && allowTools) {
            protocolMessages = trimProtocolMessages([
                ...protocolMessages,
                assistantToolMessage,
                ...results.items.map(({ action, result }) => ({
                    role: "tool" as const,
                    toolCallId: action.id,
                    name: action.name,
                    content: JSON.stringify(result),
                })),
            ]);
        } else {
            protocolMessages = trimProtocolMessages([
                ...protocolMessages,
                assistantToolMessage,
                {
                    role: "user" as const,
                    content: "工具执行结果（只可依据这些真实结果继续）：\n" + JSON.stringify(results.items.map(({ action, result }) => ({ tool: action.name, id: action.id, result }))),
                },
            ]);
        }
        input.onCheckpoint?.({ state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) });
    }

    const reply = "本轮已达到安全操作步数上限，当前已完成的节点和任务都已保存。你可以让我继续下一步。";
    protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: reply }]);
    return { reply, state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) };
}

async function runServerCanvasAgent(input: RunCanvasAgentInput): Promise<RunCanvasAgentResult> {
    let state = { ...createCanvasAgentState(), ...(input.initialState || {}) };
    let protocolMessages = trimProtocolMessages([
        ...(Array.isArray(input.protocolMessages) ? input.protocolMessages : []),
        ...(input.existingRun ? [] : [{ role: "user" as const, content: buildUserContent(input.userText, input.references, input.config.textModel || input.config.model) }]),
    ]);
    const context = input.getContext(state);
    const token = useUserStore.getState().token || undefined;
    if (!token) throw new Error("Agent 服务端运行需要先登录；未登录时仍可使用对话模式和本地画布。")
    let run = input.existingRun;
    if (!run) try {
        run = await createCanvasAgentRun({
        token,
        sessionId: input.sessionId,
        canvasId: context.project.id,
        skillId: input.skillId,
        phase: state.phase,
        config: input.config,
        message: input.userText + (input.references.length ? "\n\n本次明确引用节点：" + input.references.map((item) => `${item.id}（${item.title}）`).join("、") : ""),
        canvasContext: context,
        });
    } catch (error) {
        if (!(error instanceof Error) || !error.message.includes("未完成的 Agent Run")) throw error;
        run = await recoverCanvasAgentRun(input.sessionId, context.project.id, token);
    }
    input.onRunChange?.(run);
    let after = 0;
    let reply = "";
    let terminalError = "";
    let terminal = false;
    let reconnectAttempts = 0;
    const executedCallIds = new Set<string>();
    const abort = () => void cancelCanvasAgentRun(run, token);
    input.signal?.addEventListener("abort", abort, { once: true });
    try {
        while (!terminal) {
            throwIfAborted(input.signal);
            let requested: CanvasAgentRunToolCall[] = [];
            let turnText = "";
            try {
                after = await streamCanvasAgentRun(run, {
                    after,
                    token,
                    signal: input.signal,
                    onCursor: (id) => { after = id; },
                    onEvent: (event) => {
                    if (event.type === "run.status") input.onEvent?.({ status: "thinking", label: "正在由服务端 Agent 继续推理" });
                    if (event.type === "text.delta" && typeof event.data.delta === "string") {
                        turnText += event.data.delta;
                        reply += event.data.delta;
                        input.onTextDelta?.(reply);
                    }
                    if (event.type === "text.completed" && typeof event.data.text === "string" && !turnText) {
                        turnText = event.data.text;
                        reply += event.data.text;
                        input.onTextDelta?.(reply);
                    }
                    if (event.type === "tool.requested" && Array.isArray(event.data.calls)) requested = event.data.calls as CanvasAgentRunToolCall[];
                    if (event.type === "run.completed") terminal = true;
                    if (event.type === "run.failed" || event.type === "run.cancelled") {
                        terminalError = typeof event.data.error === "string" ? event.data.error : event.type === "run.cancelled" ? "Agent 已停止" : "Agent 执行失败";
                        terminal = true;
                    }
                    },
                });
                reconnectAttempts = 0;
            } catch (error) {
                if (input.signal?.aborted) throw error;
                if (++reconnectAttempts > 5) throw error;
                input.onEvent?.({ status: "waiting", label: "连接中断，正在恢复 Agent 事件" });
                await new Promise((resolve) => setTimeout(resolve, Math.min(4000, 500 * 2 ** reconnectAttempts)));
                continue;
            }
            if (terminal) break;
            const calls = requested.filter((call) => !executedCallIds.has(call.id));
            if (!calls.length) throw new Error("Agent 事件流结束，但没有收到终态或待执行工具");
            const actions = calls.map((call) => normalizeCanvasAgentAction(call.name, call.arguments, call.id));
            input.onEvent?.({ status: "running", label: actions.length === 1 ? canvasAgentActionLabel(actions[0]) : `正在执行 ${actions.length} 个画布操作` });
            protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: turnText || undefined, toolCalls: actions.map((action) => ({ id: action.id, name: action.name, arguments: action.arguments })) }]);
            const results = await executeActions(actions, state, async (action) => {
                const cached = input.toolResults?.[action.id];
                if (cached) return cached;
                const recovering = input.toolExecutions?.[action.id]?.status === "started";
                if (!recovering) input.onToolStart?.(action.id);
                const result = await input.executeAction(recovering ? { ...action, recovery: true } : action);
                input.onToolResult?.(action.id, result);
                return result;
            }, input.signal, input.onEvent);
            state = results.state;
            calls.forEach((call) => executedCallIds.add(call.id));
            protocolMessages = trimProtocolMessages([...protocolMessages, ...results.items.map(({ action, result }) => ({ role: "tool" as const, toolCallId: action.id, name: action.name, content: JSON.stringify(result) }))]);
            input.onCheckpoint?.({ state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) });
            await submitCanvasAgentToolResults(run, results.items.map(({ action, result }) => ({ callId: action.id, name: action.name, result })), token);
        }
    } finally {
        input.signal?.removeEventListener("abort", abort);
        if (input.signal?.aborted) input.onRunChange?.(undefined);
    }
    if (terminalError) {
        input.onRunChange?.(undefined);
        const error = new Error(terminalError);
        if (input.signal?.aborted) error.name = "AbortError";
        throw error;
    }
    const finalReply = reply.trim() || "Agent 已完成本轮处理。";
    input.onRunChange?.(undefined);
    protocolMessages = trimProtocolMessages([...protocolMessages, { role: "assistant" as const, content: finalReply }]);
    return { reply: finalReply, state, protocolMessages: persistCanvasAgentProtocolMessages(protocolMessages) };
}

async function executeActions(
    actions: CanvasAgentAction[],
    initialState: CanvasAgentState,
    executeAction: (action: CanvasAgentAction) => Promise<CanvasAgentToolResult>,
    signal?: AbortSignal,
    onEvent?: (event: CanvasAgentRuntimeEvent) => void,
) {
    let state = initialState;
    const executeOne = async (action: CanvasAgentAction) => {
        throwIfAborted(signal);
        onEvent?.({ status: "running", label: canvasAgentActionLabel(action) });
        try {
            const result = await executeAction(action);
            if (action.name === "set_agent_state" && result.ok) state = applyAgentState(state, action.arguments);
            else state = applyTaskResult(state, result);
            return { action, result };
        } catch (error) {
            return {
                action,
                result: {
                    ok: false,
                    code: "tool_execution_failed",
                    message: error instanceof Error ? error.message : "工具执行失败",
                } satisfies CanvasAgentToolResult,
            };
        }
    };

    const items = await actions.reduce<Promise<Array<{ action: CanvasAgentAction; result: CanvasAgentToolResult }>>>(
        async (pending, action) => [...(await pending), await executeOne(action)],
        Promise.resolve([]),
    );
    return { items, state };
}

function buildUserContent(text: string, references: CanvasAssistantReference[], modelName: string): CanvasAgentContent {
    const referenceText = references.length ? "\n\n本次明确引用的真实节点：" + references.map((item) => item.id + "（" + item.title + "）").join("、") : "";
    const images = supportsCanvasAgentImageInput(modelName)
        ? references.flatMap((item) => {
            const url = item.dataUrl;
            return url && (/^data:image\//.test(url) || /^https?:\/\//.test(url)) ? [{ type: "image_url" as const, image_url: { url } }] : [];
        })
        : [];
    if (!images.length) return text + referenceText;
    return [{ type: "text", text: text + referenceText }, ...images];
}

function supportsCanvasAgentImageInput(modelName: string) {
    const model = modelName.trim().toLowerCase();
    return /gpt-(?:4o|4\.1|5)|(?:^|[\\/_-])o[134](?:[\\/_-]|$)|gemini|claude|qwen.*(?:vl|vision)|glm-4v|pixtral|llava|internvl|deepseek.*vl|vision/.test(model);
}

function looksLikeClarifyingQuestion(text: string) {
    return /[?？]|请(?:告诉|选择|确认|提供)|需要.{0,12}(?:吗|呢)|希望.{0,12}(?:吗|呢)/.test(text);
}

function persistCanvasAgentProtocolMessages(messages: CanvasAgentProtocolMessage[]) {
    return messages.map((message): CanvasAgentProtocolMessage => {
        if ((message.role === "user" || message.role === "system") && Array.isArray(message.content)) {
            const text = message.content
                .filter((item) => item.type === "text")
                .map((item) => item.text)
                .join("\n")
                .trim();
            return { role: message.role, content: text || "本轮包含图片引用；媒体内容未写入会话记录。" };
        }
        return message;
    });
}

function applyAgentState(state: CanvasAgentState, patch: Record<string, unknown>): CanvasAgentState {
    return {
        ...state,
        phase: typeof patch.phase === "string" ? (patch.phase as CanvasAgentState["phase"]) : state.phase,
        brief: typeof patch.brief === "string" ? patch.brief : state.brief,
        targetDurationSeconds: typeof patch.targetDurationSeconds === "number" ? patch.targetDurationSeconds : state.targetDurationSeconds,
        approvedPlan: typeof patch.approvedPlan === "string" ? patch.approvedPlan : state.approvedPlan,
        approvedNodeIds: Array.isArray(patch.approvedNodeIds) ? (patch.approvedNodeIds as string[]) : state.approvedNodeIds,
        referenceNodeIds: Array.isArray(patch.referenceNodeIds) ? (patch.referenceNodeIds as string[]) : state.referenceNodeIds,
    };
}

function applyTaskResult(state: CanvasAgentState, result: CanvasAgentToolResult): CanvasAgentState {
    const taskId = typeof result.taskId === "string" ? result.taskId : "";
    if (!taskId) return state;
    const completed = result.status === "success" || result.status === "completed";
    const terminal = completed || result.status === "error" || result.status === "failed";
    return {
        ...state,
        pendingTaskIds: terminal ? state.pendingTaskIds.filter((id) => id !== taskId) : [...new Set([...state.pendingTaskIds, taskId])],
        completedTaskIds: completed ? [...new Set([...state.completedTaskIds, taskId])] : state.completedTaskIds,
    };
}

function throwIfAborted(signal?: AbortSignal) {
    if (!signal?.aborted) return;
    const error = new Error("Agent 已停止");
    error.name = "AbortError";
    throw error;
}
