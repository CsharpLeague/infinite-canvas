export function friendlyAIErrorMessage(value: unknown, fallback = "请求失败") {
    const raw = extractErrorText(value).trim();
    if (!raw) return fallback;
    const text = raw.toLowerCase();

    if (text.includes("real person")) {
        const contentIndex = raw.match(/content\[(\d+)\]/i)?.[1];
        const imageLabel = contentIndex ? `第 ${Math.max(1, Number(contentIndex))} 张参考图片` : "参考图片";
        return `${imageLabel}可能包含真人，已被平台安全审核拒绝。请改用明显的虚构、动漫或非写实角色图片。`;
    }
    if (includesAny(text, ["content policy", "safety policy", "safety check", "moderation", "unsafe content", "content violation"])) {
        return "输入内容未通过平台安全审核，请调整提示词或更换参考素材后重试。";
    }
    if (text.includes("reference_audio cannot be the only reference input")) {
        return "火山方舟不支持仅使用参考音频，请同时连接至少一张参考图片或一段参考视频。";
    }
    if (includesAny(text, ["invalid api key", "incorrect api key", "invalid_api_key", "unauthorized"]) || /\b401\b/.test(text)) {
        return "API Key 无效或已失效，请检查渠道密钥和模型权限。";
    }
    if (includesAny(text, ["insufficient quota", "insufficient balance", "quota exceeded", "余额不足"])) {
        return "模型渠道余额或额度不足，请充值或更换渠道后重试。";
    }
    if (includesAny(text, ["rate limit", "too many requests"]) || /\b429\b/.test(text)) {
        return "模型渠道请求过于频繁，请稍后重试。";
    }
    if (includesAny(text, ["circuit breaker", "temporarily suspended", "channel_circuit_open"])) {
        return "当前模型渠道暂时不可用，正在等待上游恢复，请稍后重试或更换渠道。";
    }
    if (includesAny(text, ["bad gateway", "error code: 502"]) || /\b502\b/.test(text)) {
        return "上游模型网关暂时不可用，请稍后重试或更换支持图片理解的模型渠道。";
    }
    if (includesAny(text, ["gateway time-out", "gateway timeout", "upstream timeout", "timed out", "timeout"]) || /\b504\b/.test(text)) {
        return "上游模型生成超时，请稍后重试、降低生成规格或更换渠道。";
    }
    if (includesAny(text, ["network error", "connection refused", "connection reset", "no such host"])) {
        return "无法连接模型渠道，请检查接口地址和服务器网络。";
    }
    return raw;
}

function extractErrorText(value: unknown): string {
    if (typeof value === "string") return value;
    if (value instanceof Error) return value.message;
    if (!value || typeof value !== "object") return "";
    const record = value as Record<string, unknown>;
    return extractErrorText(record.message) || extractErrorText(record.msg) || extractErrorText(record.error) || extractErrorText(record.detail);
}

function includesAny(text: string, values: string[]) {
    return values.some((value) => text.includes(value));
}
