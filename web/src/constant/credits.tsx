import type { ComponentProps } from "react";
import { Zap } from "lucide-react";

export function CreditSymbol({ className, ...props }: ComponentProps<"span">) {
    return (
        <span {...props} className={`inline-flex items-center justify-center ${className || ""}`}>
            <Zap className="size-[1em] fill-current" strokeWidth={2.4} />
        </span>
    );
}

export type ModelCreditCost = {
    model: string;
    billingMode?: "fixed" | "video";
    credits: number;
    videoRates?: {
        "480p"?: number;
        "720p"?: number;
        "1080p"?: number;
    };
};

export function modelCreditCost(modelCosts: ModelCreditCost[] | undefined, model: string) {
    return modelCosts?.find((item) => item.model === model)?.credits || 0;
}

export function requestCreditCost(options: { channelMode: string; modelCosts?: ModelCreditCost[]; model: string; count?: string | number; mode?: string; resolution?: string; size?: string; seconds?: string | number }) {
    if (options.channelMode !== "remote") return 0;
    const item = options.modelCosts?.find((cost) => cost.model === options.model);
    if (options.mode === "video" && item?.billingMode === "video") {
        const resolution = videoCreditResolution(options.resolution, options.size);
        const rate = Number(item.videoRates?.[resolution]) || 0;
        const seconds = Math.max(1, Math.ceil(Number(options.seconds) || 0));
        return rate > 0 ? rate * seconds : Math.max(0, Number(item.credits) || 0);
    }
    const count = Math.max(1, Math.floor(Math.abs(Number(options.count)) || 1));
    return (item?.credits || 0) * count;
}

function videoCreditResolution(resolution?: string, size?: string): "480p" | "720p" | "1080p" {
    const selected = String(resolution || "").toLowerCase();
    if (selected.includes("1080")) return "1080p";
    if (selected.includes("720")) return "720p";
    if (selected.includes("480")) return "480p";
    const dimensions = String(size || "").toLowerCase().match(/(\d+)\s*x\s*(\d+)/);
    const shortSide = dimensions ? Math.min(Number(dimensions[1]), Number(dimensions[2])) : 0;
    if (shortSide >= 900) return "1080p";
    if (shortSide >= 600) return "720p";
    return "480p";
}
