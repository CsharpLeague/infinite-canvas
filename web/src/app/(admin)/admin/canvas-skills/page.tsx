"use client";

import { DeleteOutlined, EditOutlined, ImportOutlined, PlusOutlined, SearchOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Card, Flex, Form, Input, InputNumber, Modal, Select, Space, Table, Tag, Typography, Upload, message, type TableColumnsType } from "antd";
import { useState } from "react";

import { deleteAdminCanvasSkill, fetchAdminCanvasSkills, importAdminCanvasSkill, saveAdminCanvasSkill, type AdminCanvasSkill } from "@/services/api/admin";
import { useUserStore } from "@/stores/use-user-store";

type SkillForm = Partial<AdminCanvasSkill>;

const canvasToolOptions = [
    "get_canvas_summary", "get_selected_nodes", "get_node", "get_upstream_nodes", "get_downstream_nodes", "get_connected_nodes",
    "get_generation_config", "get_generation_task", "set_agent_state", "create_primary_script_node", "create_text_node", "update_text_node",
    "update_node", "delete_node", "create_connection", "delete_connection", "create_group", "arrange_nodes", "generate_image", "edit_image",
    "generate_video", "generate_audio", "get_media_task_status",
].map((value) => ({ value, label: value }));

export default function AdminCanvasSkillsPage() {
    const token = useUserStore((state) => state.token);
    const [messageApi, contextHolder] = message.useMessage();
    const queryClient = useQueryClient();
    const [keyword, setKeyword] = useState("");
    const [search, setSearch] = useState("");
    const [editing, setEditing] = useState<Partial<AdminCanvasSkill> | null>(null);
    const [form] = Form.useForm<SkillForm>();
    const query = useQuery({ queryKey: ["admin", "canvas-skills", search], queryFn: () => fetchAdminCanvasSkills(token, search), enabled: Boolean(token) });
    const saveMutation = useMutation({
        mutationFn: (skill: Partial<AdminCanvasSkill>) => saveAdminCanvasSkill(token, skill),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ["admin", "canvas-skills"] });
            setEditing(null);
            messageApi.success("Skill 已保存");
        },
        onError: (error) => messageApi.error(error instanceof Error ? error.message : "保存失败"),
    });
    const deleteMutation = useMutation({
        mutationFn: (id: string) => deleteAdminCanvasSkill(token, id),
        onSuccess: async () => {
            await queryClient.invalidateQueries({ queryKey: ["admin", "canvas-skills"] });
            messageApi.success("Skill 已删除");
        },
        onError: (error) => messageApi.error(error instanceof Error ? error.message : "删除失败"),
    });
    const importMutation = useMutation({
        mutationFn: (file: File) => importAdminCanvasSkill(token, file),
        onSuccess: async (skill) => {
            await queryClient.invalidateQueries({ queryKey: ["admin", "canvas-skills"] });
            openEditor(skill);
            messageApi.success("Skill 包已导入为草稿，请配置工具权限后发布");
        },
        onError: (error) => messageApi.error(error instanceof Error ? error.message : "导入失败"),
    });

    const openEditor = (skill: Partial<AdminCanvasSkill> = { status: "draft", category: "通用", sort: 0, allowedTools: [] }) => {
        const next = { status: "draft" as const, category: "通用", sort: 0, allowedTools: [], ...skill };
        form.resetFields();
        form.setFieldsValue(next);
        setEditing(next);
    };

    const columns: TableColumnsType<AdminCanvasSkill> = [
        { title: "名称", dataIndex: "name", render: (_, item) => <Flex vertical><Typography.Text strong>{item.name}</Typography.Text><Typography.Text type="secondary">/{item.slug}</Typography.Text></Flex> },
        { title: "分类", dataIndex: "category", width: 140 },
        { title: "工具数", dataIndex: "allowedTools", width: 90, render: (items: string[]) => items?.length || 0 },
        { title: "参考文件", dataIndex: "files", width: 100, render: (files: Record<string, string>) => Math.max(0, Object.keys(files || {}).length - 1) },
        { title: "状态", dataIndex: "status", width: 100, render: (status) => <Tag color={status === "published" ? "green" : "default"}>{status === "published" ? "已发布" : "草稿"}</Tag> },
        { title: "排序", dataIndex: "sort", width: 80 },
        { title: "操作", width: 110, align: "right", render: (_, item) => <Space><Button type="text" icon={<EditOutlined />} onClick={() => openEditor(item)} /><Button danger type="text" icon={<DeleteOutlined />} onClick={() => Modal.confirm({ title: `删除「${item.name}」？`, content: "删除后画布将无法再选择此 Skill。", okButtonProps: { danger: true }, onOk: () => deleteMutation.mutateAsync(item.id) })} /></Space> },
    ];

    return (
        <main style={{ padding: 24 }}>
            {contextHolder}
            <Flex vertical gap={16}>
                <Card variant="borderless">
                    <Flex justify="space-between" gap={16} wrap>
                        <Input.Search value={keyword} onChange={(event) => setKeyword(event.target.value)} onSearch={setSearch} prefix={<SearchOutlined />} placeholder="搜索名称或标识" allowClear style={{ maxWidth: 360 }} />
                        <Space>
                            <Upload accept="application/zip,.zip,application/json,.json" showUploadList={false} beforeUpload={async (file) => {
                                try {
                                    if (file.name.toLowerCase().endsWith(".zip")) await importMutation.mutateAsync(file);
                                    else {
                                        const imported = JSON.parse(await file.text()) as Partial<AdminCanvasSkill>;
                                        openEditor({ ...imported, id: undefined, status: imported.status === "published" ? "published" : "draft" });
                                    }
                                } catch {
                                    if (!file.name.toLowerCase().endsWith(".zip")) messageApi.error("无法解析 Skill JSON");
                                }
                                return false;
                            }}><Button loading={importMutation.isPending} icon={<ImportOutlined />}>导入 Skill</Button></Upload>
                            <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>新建 Skill</Button>
                        </Space>
                    </Flex>
                </Card>
                <Card variant="borderless"><Table rowKey="id" columns={columns} dataSource={query.data?.items || []} loading={query.isFetching} pagination={false} /></Card>
            </Flex>
            <Modal forceRender title={editing?.id ? "编辑 Skill" : "新建 Skill"} open={editing !== null} width={760} okText="保存" confirmLoading={saveMutation.isPending} onCancel={() => setEditing(null)} onOk={async () => {
                const value = await form.validateFields();
                await saveMutation.mutateAsync({ ...editing, ...value });
            }}>
                <Form form={form} layout="vertical" preserve={false}>
                    <Flex gap={16}><Form.Item name="name" label="名称" rules={[{ required: true }]} style={{ flex: 1 }}><Input placeholder="例如：短视频分镜" /></Form.Item><Form.Item name="slug" label="唯一标识" rules={[{ required: true }]} style={{ flex: 1 }}><Input placeholder="storyboard-video" /></Form.Item></Flex>
                    <Flex gap={16}><Form.Item name="category" label="分类" rules={[{ required: true }]} style={{ flex: 1 }}><Input /></Form.Item><Form.Item name="status" label="状态" style={{ width: 150 }}><Select options={[{ label: "草稿", value: "draft" }, { label: "已发布", value: "published" }]} /></Form.Item><Form.Item name="sort" label="排序" style={{ width: 110 }}><InputNumber min={0} style={{ width: "100%" }} /></Form.Item></Flex>
                    <Form.Item name="description" label="简介"><Input.TextArea rows={2} placeholder="用于 Skill 选择器的能力说明" /></Form.Item>
                    <Form.Item name="placeholder" label="输入提示"><Input placeholder="选择后显示在画布输入框中" /></Form.Item>
                    <Form.Item name="allowedTools" label="允许的画布工具" extra="Skill 只能调用这里明确选择的工具；留空表示只生成文本。"><Select mode="multiple" allowClear maxTagCount="responsive" options={canvasToolOptions} placeholder="选择工具权限" /></Form.Item>
                    <Form.Item name="instructions" label="运行指令" rules={[{ required: true }]}><Input.TextArea rows={12} placeholder="写清目标、步骤、约束和输出要求。不会执行上传的脚本。" /></Form.Item>
                </Form>
            </Modal>
        </main>
    );
}
