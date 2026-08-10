import { apiGet } from "@/services/api/request";

export type CanvasSkillSummary = {
    id: string;
    slug: string;
    name: string;
    description: string;
    category: string;
    icon: string;
    placeholder: string;
};

export function fetchCanvasSkills() {
    return apiGet<CanvasSkillSummary[]>("/api/canvas-skills");
}
