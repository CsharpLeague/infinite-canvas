"use client";

import { useEffect, useState } from "react";
import { LoaderCircle } from "lucide-react";

import { formatDuration } from "@/lib/image-utils";
import { cn } from "@/lib/utils";

const pendingMessages = ["正在创建图片", "马上就好了", "再等等", "正在整理细节"];

export function ImageGenerationPending({ className, label, compact = false }: { className?: string; label?: string; compact?: boolean }) {
    const [elapsedMs, setElapsedMs] = useState(0);

    useEffect(() => {
        const startedAt = Date.now();
        const timer = window.setInterval(() => setElapsedMs(Date.now() - startedAt), compact ? 100 : 1000);
        return () => window.clearInterval(timer);
    }, [compact]);

    const tick = Math.floor(elapsedMs / 1000);
    const index = Math.floor(tick / 2) % pendingMessages.length;
    const progress = Math.min(98, 10 + (1 - Math.exp(-tick / 28)) * 88);

    if (compact) {
        return (
            <div className={cn("flex min-h-5 items-center gap-2 py-0.5 text-xs opacity-55", className)}>
                <LoaderCircle className="size-3.5 shrink-0 animate-spin" />
                <span>{label || "正在思考"} {(elapsedMs / 1000).toFixed(1)}s…</span>
            </div>
        );
    }

    return (
        <div className={cn("relative aspect-[4/3] overflow-hidden bg-stone-100 dark:bg-white/10", className)}>
            <div
                className="absolute inset-0 opacity-60"
                style={{
                    backgroundImage: "radial-gradient(circle, rgba(120,113,108,0.35) 1.4px, transparent 1.6px)",
                    backgroundSize: "16px 16px",
                    maskImage: "radial-gradient(ellipse at 38% 68%, black 0%, black 28%, transparent 60%)",
                }}
            />
            <div className="absolute left-4 top-4 flex items-center gap-2 text-[15px] font-medium text-stone-500 dark:text-stone-300">
                <LoaderCircle className="size-4 animate-spin" />
                <span>{label || pendingMessages[index]}</span>
            </div>
            <div className="absolute bottom-4 left-4 right-4">
                <div className="mb-2 flex items-center justify-between text-xs text-stone-500 dark:text-stone-400">
                    <span>{formatDuration(tick * 1000)}</span>
                    <span>{Math.floor(progress)}%</span>
                </div>
                <div className="h-1.5 rounded-full bg-stone-300/70 dark:bg-white/12">
                    <div className="h-full rounded-full bg-stone-900 dark:bg-stone-100" style={{ width: `${progress}%` }} />
                </div>
            </div>
        </div>
    );
}
