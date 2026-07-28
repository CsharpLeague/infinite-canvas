---
title: 待测试
description: 当前版本已实现但仍需人工验证的变更项
---

# 待测试

## 模型渠道类型与结果兼容

- 管理后台渠道类型支持通用 OpenAI、New API、Sub2API、火山方舟和 KIE。
- 文本模型继续使用最小文本请求测试；图片、视频和 KIE 模型改为非收费的连接、鉴权、模型或配置检查。
- Sub2API 文本渠道使用 Responses 接口测试。
- 图片结果解析兼容标准 `data[].url`、`data[].b64_json`，以及常见的 `images`、`result`、`output`、`image_url`、`base64` 嵌套返回，最终保持现有生图结果结构。
- 图片生成与编辑接口兼容 SSE 响应头缺失或错误的 New API 中转返回，可从 `image_generation.completed` / `image_edit.completed` 事件中提取 Base64 图片。
- Images API 默认明确请求 `response_format: url`，仅在用户开启 Base64 返回时改为 `b64_json`；流式传输保持默认关闭。
- AI 调用日志会脱敏完整或被截断的超长 Base64 图片内容，避免返回详情被图片数据占满。
- 火山引擎 TOS 存储自动使用 S3 Endpoint 和 VirtualHostStyle 请求，修复上传及容量统计的 `InvalidPathAccess` 错误；其他 S3/R2 配置继续使用原路径方式。

## 火山方舟视频生成

- Seedance 视频渠道继续使用项目统一的 `/videos` 创建与 `/videos/{id}` 查询入口，后端转发到火山方舟官方 `/api/v3/contents/generations/tasks`。
- 画布的通用 multipart 请求会转换为方舟官方 JSON `content` 结构，包含文本提示词、比例、分辨率、时长、音频生成开关、首尾帧以及图片、视频、音频参考素材。
- 已经符合方舟官方格式的 JSON 请求保持原样直通；创建结果 `id`、任务状态和 `content.video_url` 继续转换为项目现有视频任务结果。
- 任务完成响应会明确读取火山方舟嵌套的 `content.video_url`，避免上游已返回视频但画布提示“没有返回视频地址”。
- 本地图片文件可转换为 Base64 Data URL；参考视频和音频需先上传云存储并提供可公开访问的 URL。
