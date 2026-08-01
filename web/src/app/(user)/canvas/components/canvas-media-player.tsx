"use client";

import type { CSSProperties, PointerEvent } from "react";
import "media-chrome/lang/zh-CN";
import { MediaController, MediaFullscreenButton, MediaMuteButton, MediaPlayButton, MediaTimeDisplay, MediaTimeRange, MediaVolumeRange } from "media-chrome/react";
import { setLanguage } from "media-chrome/utils/i18n";

setLanguage("zh-CN");

type Props = {
    src: string;
    kind: "video" | "audio";
    title?: string;
    surface?: string;
    textColor?: string;
    borderColor?: string;
};

const controlStyle = {
    "--media-control-background": "transparent",
    "--media-control-hover-background": "rgba(255,255,255,.12)",
    "--media-control-padding": "6px",
    "--media-icon-color": "#fff",
    "--media-primary-color": "#2dd4bf",
    color: "#fff",
} as CSSProperties;

export function CanvasMediaPlayer({ src, kind, title, surface, textColor, borderColor }: Props) {
    const stopCanvas = (event: PointerEvent<HTMLElement>) => event.stopPropagation();

    if (kind === "audio")
        return (
            <MediaController
                data-canvas-no-zoom
                audio
                className="w-full overflow-hidden rounded-xl border"
                style={{ ...controlStyle, "--media-icon-color": textColor, "--media-primary-color": textColor, background: surface, borderColor }}
                onPointerDown={stopCanvas}
            >
                <audio slot="media" src={src} preload="metadata" title={title} />
                <div className="flex h-12 items-center gap-1 px-1.5">
                    <MediaPlayButton noTooltip className="shrink-0 rounded-lg" style={controlStyle} />
                    <MediaTimeDisplay showDuration className="shrink-0 text-[11px] tabular-nums" style={controlStyle} />
                    <MediaTimeRange className="min-w-0 flex-1" style={controlStyle} />
                    <MediaMuteButton noTooltip className="shrink-0 rounded-lg" style={controlStyle} />
                </div>
            </MediaController>
        );

    return (
        <MediaController data-canvas-no-zoom className="group/media relative block size-full overflow-hidden rounded-[18px] bg-black" style={controlStyle} onPointerDown={stopCanvas}>
            <video slot="media" src={src} preload="metadata" playsInline className="size-full object-contain" title={title} />
            <MediaPlayButton
                noTooltip
                className="absolute left-1/2 top-1/2 z-10 flex size-12 -translate-x-1/2 -translate-y-1/2 items-center justify-center rounded-full opacity-0 shadow-xl backdrop-blur-md transition hover:scale-105 [&[mediapaused]]:opacity-100"
                style={{ ...controlStyle, "--media-control-background": "rgba(255,255,255,.92)", "--media-icon-color": "#171717", color: "#171717" } as CSSProperties}
            />
            <div className="pointer-events-none absolute inset-x-0 bottom-0 bg-gradient-to-t from-black/85 via-black/45 to-transparent px-2.5 pb-2 pt-10 opacity-0 transition-opacity duration-200 group-hover/media:opacity-100">
                <div className="pointer-events-auto">
                    <MediaTimeRange className="block w-full" style={controlStyle} />
                    <div className="flex h-8 items-center gap-1">
                        <MediaPlayButton noTooltip className="shrink-0 rounded-lg" style={controlStyle} />
                        <MediaTimeDisplay showDuration className="shrink-0 text-[11px] tabular-nums" style={controlStyle} />
                        <span className="flex-1" />
                        <MediaMuteButton noTooltip className="shrink-0 rounded-lg" style={controlStyle} />
                        <MediaVolumeRange className="w-20" style={controlStyle} />
                        <MediaFullscreenButton noTooltip className="shrink-0 rounded-lg" style={controlStyle} />
                    </div>
                </div>
            </div>
        </MediaController>
    );
}
