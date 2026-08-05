import { apiDelete, apiGet, apiPost } from "./request";
import { useUserStore } from "@/stores/use-user-store";

export type VirtualPortraitStatus = "processing" | "active" | "failed";
export type VirtualPortraitTask = {
    id: string;
    channelId: string;
    sourceUrl: string;
    sourceFingerprint: string;
    name: string;
    groupId: string;
    assetId: string;
    status: VirtualPortraitStatus;
    error: string;
    createdAt: string;
    updatedAt: string;
};

export function createVirtualPortrait(channelId: string, sourceUrl: string, name: string) {
    return apiPost<VirtualPortraitTask>("/api/v1/virtual-portraits", { channelId, sourceUrl, name }, useUserStore.getState().token);
}

export function fetchVirtualPortrait(id: string) {
    return apiGet<VirtualPortraitTask>(`/api/v1/virtual-portraits/${encodeURIComponent(id)}`, undefined, useUserStore.getState().token);
}

export function deleteVirtualPortrait(id: string) {
    return apiDelete<boolean>(`/api/v1/virtual-portraits/${encodeURIComponent(id)}`, useUserStore.getState().token);
}
