"use client";

import { useMemo, useState } from "react";
import { ArrowUp, Bot, ChevronDown, FileText, FolderOpen, ImageIcon, Menu, MessageCircle, Music2, Sparkles, Square, Upload, Video, X } from "lucide-react";
import { Button, Dropdown, Popover } from "antd";

import { canvasThemes } from "@/lib/canvas-theme";
import { useEffectiveConfig } from "@/stores/use-config-store";
import { useThemeStore } from "@/stores/use-theme-store";
import { CanvasNodeType, type CanvasAgentConfig, type CanvasAssistantMode, type CanvasAssistantReference } from "../types";
import { isCanvasImageNodeType } from "../utils/canvas-panorama";
import { CanvasImageSettingsPopover } from "./canvas-image-settings-popover";
import { CanvasVideoSettingsPopover } from "./canvas-video-settings-popover";
import type { CanvasSkillSummary } from "@/services/api/canvas-skills";

export type CanvasAssistantComposerProps = {
    prompt: string;
    isRunning: boolean;
    mode: CanvasAssistantMode;
    references: CanvasAssistantReference[];
    agentConfig: CanvasAgentConfig;
    skills?: CanvasSkillSummary[];
    selectedSkillId?: string;
    onAgentConfigChange: (patch: Partial<CanvasAgentConfig>) => void;
    onSkillChange?: (skillId?: string) => void;
    onModeChange: (mode: CanvasAssistantMode) => void;
    onPromptChange: (prompt: string) => void;
    onSubmit: () => void | Promise<void>;
    onStop?: () => void;
    onOpenUpload: () => void;
    onOpenAssets: () => void;
    onRemoveReference: (id: string) => void;
    onPasteImage: (file: File) => void;
};

export function CanvasAssistantComposer({
    prompt,
    isRunning,
    mode,
    references,
    agentConfig,
    skills = [],
    selectedSkillId,
    onAgentConfigChange,
    onSkillChange,
    onModeChange,
    onPromptChange,
    onSubmit,
    onStop,
    onOpenUpload,
    onOpenAssets,
    onRemoveReference,
    onPasteImage,
}: CanvasAssistantComposerProps) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    const effectiveConfig = useEffectiveConfig();
    const imageConfig = useMemo(() => ({ ...effectiveConfig, quality: agentConfig.imageQuality, size: agentConfig.imageSize }), [agentConfig.imageQuality, agentConfig.imageSize, effectiveConfig]);
    const videoConfig = useMemo(() => ({ ...effectiveConfig, vquality: agentConfig.videoQuality, size: agentConfig.videoSize }), [agentConfig.videoQuality, agentConfig.videoSize, effectiveConfig]);
    const [skillOpen, setSkillOpen] = useState(false);
    const selectedSkill = skills.find((item) => item.id === selectedSkillId);
    const skillGroups = useMemo(() => Array.from(new Set(skills.map((item) => item.category || "通用"))).map((category) => ({ category, items: skills.filter((item) => (item.category || "通用") === category) })), [skills]);

    return (
        <div className="px-2 pb-2" onWheelCapture={(event) => event.stopPropagation()}>
            {references.length ? (
                <div className="thin-scrollbar mb-1.5 flex max-w-full gap-1.5 overflow-x-auto px-1 pb-1">
                    {references.map((item) => (
                        <AssistantReferenceChip key={item.id} item={item} onRemove={() => onRemoveReference(item.id)} />
                    ))}
                </div>
            ) : null}
            <div className="rounded-2xl border px-3 pb-3 pt-3" style={{ background: theme.toolbar.panel, borderColor: theme.node.stroke }}>
                {mode === "agent" ? (
                    <div className="mb-2 flex items-center justify-between gap-2 border-b pb-2" style={{ borderColor: theme.node.stroke }}>
                        <Popover
                            trigger="click"
                            placement="topLeft"
                            open={skillOpen}
                            onOpenChange={setSkillOpen}
                            content={<div className="thin-scrollbar max-h-80 w-72 overflow-y-auto p-1">
                                <div className="px-2 pb-2 text-xs" style={{ color: theme.node.muted }}>选择一项能力，Agent 将按对应方法和工具权限执行</div>
                                {skillGroups.map((group) => <div key={group.category} className="mb-2">
                                    <div className="px-2 py-1 text-[11px] font-medium uppercase tracking-wider" style={{ color: theme.node.muted }}>{group.category}</div>
                                    {group.items.map((skill) => <button key={skill.id} type="button" className="mb-1 block w-full rounded-lg border px-2.5 py-2 text-left text-sm font-medium transition" style={{ background: skill.id === selectedSkillId ? theme.toolbar.itemHover : "transparent", borderColor: skill.id === selectedSkillId ? theme.node.stroke : "transparent", color: theme.node.text }} onClick={() => { onSkillChange?.(skill.id); setSkillOpen(false); }}>
                                        <span className="block text-sm font-medium">{skill.name}</span>
                                    </button>)}
                                </div>)}
                            </div>}
                        >
                            <button type="button" className="flex min-w-0 items-center gap-2 rounded-lg px-1.5 py-1 text-left transition" style={{ color: theme.node.text }}>
                                <span className="grid size-7 shrink-0 place-items-center rounded-md" style={{ background: theme.toolbar.itemHover }}><Sparkles className="size-3.5" /></span>
                                <span className="min-w-0 truncate text-xs font-medium">{selectedSkill?.name || "选择 Skill"}</span>
                                <ChevronDown className="size-3.5 shrink-0" />
                            </button>
                        </Popover>
                        <span className="flex shrink-0 items-center gap-1.5 text-[11px]" style={{ color: theme.node.muted }}><span className={`size-1.5 rounded-full ${isRunning ? "animate-pulse" : ""}`} style={{ background: isRunning ? theme.node.text : theme.node.stroke }} />{isRunning ? "运行中" : "待命"}</span>
                    </div>
                ) : null}
                <textarea
                    value={prompt}
                    onChange={(event) => onPromptChange(event.target.value)}
                    onPaste={(event) => {
                        const file = Array.from(event.clipboardData.files).find((item) => item.type.startsWith("image/"));
                        if (!file) return;
                        event.preventDefault();
                        onPasteImage(file);
                    }}
                    onKeyDown={(event) => {
                        if (event.key !== "Enter" || event.ctrlKey || event.metaKey || event.shiftKey) return;
                        event.preventDefault();
                        void onSubmit();
                    }}
                    className="thin-scrollbar h-20 w-full resize-none border-0 bg-transparent px-1 py-1 text-sm leading-5 outline-none placeholder:opacity-40"
                    style={{ color: theme.node.text }}
                    placeholder={mode === "chat" ? "讨论创意、分析内容，不会操作画布" : selectedSkill?.placeholder || "描述要执行的任务，Agent 可以操作画布"}
                />
                <div className="mt-2 flex items-center justify-between gap-2">
                    <div className="flex min-w-0 flex-1 items-center gap-1">
                        <Dropdown
                            trigger={["click"]}
                            menu={{
                                items: [
                                    { key: "upload", icon: <Upload className="size-4" />, label: "上传文件" },
                                    { key: "assets", icon: <FolderOpen className="size-4" />, label: "我的素材" },
                                ],
                                onClick: ({ key }) => (key === "upload" ? onOpenUpload() : onOpenAssets()),
                            }}
                        >
                            <Button type="text" shape="circle" className="!h-8 !w-8 !min-w-8" style={{ color: theme.node.text }} icon={<Menu className="size-4" />} aria-label="添加素材" />
                        </Dropdown>
                        {mode === "agent" ? <CanvasImageSettingsPopover
                            config={imageConfig}
                            placement="topLeft"
                            showCount={false}
                            buttonIcon={<ImageIcon className="size-3.5" />}
                            buttonClassName="!h-8 !max-w-[116px] !justify-start !rounded-full !px-2.5"
                            onConfigChange={(key, value) => {
                                if (key === "quality") onAgentConfigChange({ imageQuality: value });
                                else if (key === "size") onAgentConfigChange({ imageSize: value });
                            }}
                        /> : null}
                        {mode === "agent" ? <CanvasVideoSettingsPopover
                            config={videoConfig}
                            placement="topLeft"
                            visualOnly
                            buttonIcon={<Video className="size-3.5" />}
                            buttonClassName="!h-8 !max-w-[124px] !justify-start !rounded-full !px-2.5"
                            onConfigChange={(key, value) => {
                                if (key === "vquality") onAgentConfigChange({ videoQuality: value });
                                else if (key === "size") onAgentConfigChange({ videoSize: value });
                            }}
                        /> : null}
                    </div>
                    <Button
                        type="primary"
                        shape="circle"
                        className="!size-10 !min-w-10"
                        disabled={!isRunning && !prompt.trim()}
                        onClick={() => (isRunning ? onStop?.() : void onSubmit())}
                        aria-label={isRunning ? "停止" : "发送"}
                        icon={isRunning ? <Square className="size-4 fill-current" /> : <ArrowUp className="size-4" />}
                    />
                </div>
                <div className="mt-2 flex items-center gap-1 border-t pt-2" style={{ borderColor: theme.node.stroke }}>
                    {([
                        { value: "chat" as const, label: "对话", icon: MessageCircle },
                        { value: "agent" as const, label: "Agent", icon: Sparkles },
                    ]).map((item) => {
                        const Icon = item.icon;
                        const active = mode === item.value;
                        return (
                            <button
                                key={item.value}
                                type="button"
                                disabled={isRunning}
                                className="flex h-7 items-center gap-1.5 rounded-md px-2 text-xs transition disabled:cursor-not-allowed disabled:opacity-45"
                                style={{ background: active ? theme.toolbar.itemHover : "transparent", color: active ? theme.node.text : theme.node.muted }}
                                onClick={() => onModeChange(item.value)}
                                aria-pressed={active}
                                title={item.value === "chat" ? "只讨论，不操作画布" : "允许读取和操作画布"}
                            >
                                <Icon className="size-3.5" />
                                {item.label}
                            </button>
                        );
                    })}
                    <span className="ml-1 truncate text-[11px]" style={{ color: theme.node.muted }}>
                        {mode === "chat" ? "不会操作画布" : "可以操作画布"}
                    </span>
                </div>
            </div>
        </div>
    );
}

export function AssistantReferenceChip({ item, onRemove }: { item: CanvasAssistantReference; onRemove?: () => void }) {
    const theme = canvasThemes[useThemeStore((state) => state.theme)];
    return (
        <div className="group/chip relative inline-flex h-8 max-w-[160px] shrink-0 items-center gap-1.5 rounded-lg text-sm" style={{ color: theme.node.text }}>
            <span className="grid size-8 shrink-0 place-items-center overflow-hidden rounded-lg border" style={{ background: theme.node.panel, borderColor: theme.node.stroke }}>
                {item.dataUrl ? <img src={item.dataUrl} alt="" className="size-8 object-cover" /> : <ReferenceIcon type={item.type} />}
            </span>
            <span className="max-w-[112px] truncate text-xs">{item.title}</span>
            {onRemove ? (
                <button
                    type="button"
                    className="absolute -right-1 -top-1 grid size-4 place-items-center rounded-full border opacity-0 transition group-hover/chip:opacity-100"
                    style={{ background: theme.toolbar.panel, borderColor: theme.node.stroke }}
                    onClick={onRemove}
                    aria-label="移除引用"
                >
                    <X className="size-3" />
                </button>
            ) : null}
        </div>
    );
}

function ReferenceIcon({ type }: { type: CanvasNodeType }) {
    if (type === CanvasNodeType.Video) return <Video className="size-4" />;
    if (type === CanvasNodeType.Audio) return <Music2 className="size-4" />;
    if (type === CanvasNodeType.Text) return <FileText className="size-4" />;
    if (isCanvasImageNodeType(type)) return <ImageIcon className="size-4" />;
    return <Bot className="size-4" />;
}
