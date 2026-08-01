---
title: PM2 部署
description: 使用 PM2 在 Linux 服务器同时托管 Go 后端与 Next.js 前端
---

# PM2 部署

PM2 部署会运行两个进程：

- `infinite-canvas-api`：Go 后端，监听 `0.0.0.0:8080`
- `infinite-canvas-web`：Next.js 前端，监听 `0.0.0.0:3100`

浏览器只访问前端，Next.js 会把 `/api/*` 转发给本机后端。
未配置域名和 Nginx 时，可以临时在安全组开放 `3100`，通过 `http://服务器IP:3100` 访问。配置 Nginx 和 HTTPS 后，只开放 `80`、`443`，关闭公网 `3100`；不要把 `8080` 暴露到公网。

## 1. 环境要求

- Linux x64 或 arm64
- Go 1.25
- Node.js 22
- Bun 1.3
- PM2
- Nginx

安装 PM2：

```bash
npm install -g pm2
```

## 2. 获取代码

```bash
git clone https://github.com/tigerowo/infinite-canvas.git
cd infinite-canvas
```

如果部署的是本项目的定制分支，需要改为包含定制提交的仓库地址和分支。

## 3. 配置环境变量

```bash
cp .env.example .env
nano .env
```

生产环境至少修改：

```dotenv
ADMIN_USERNAME=admin
ADMIN_PASSWORD=请设置高强度密码
JWT_SECRET=请设置足够长的随机字符串
JWT_EXPIRE_HOURS=168
PORT=8080
PUBLIC_BASE_URL=https://your-domain.example.com
STORAGE_DRIVER=sqlite
DATABASE_DSN=data/infinite-canvas.db
```

`PUBLIC_BASE_URL` 必须是公网可访问的 HTTPS 站点地址，否则火山方舟无法读取本地上传的 Seedance 参考素材。

## 4. 构建并启动

```bash
chmod +x scripts/deploy-pm2.sh
./scripts/deploy-pm2.sh
```

脚本会：

1. 编译 Go 后端到 `bin/infinite-canvas-api`
2. 使用 Bun 安装前端依赖
3. 构建 Next.js standalone 产物并复制静态资源
4. 启动或平滑重载两个 PM2 进程
5. 保存 PM2 进程列表

查看状态和日志：

```bash
pm2 status
pm2 logs infinite-canvas-api
pm2 logs infinite-canvas-web
```

配置开机启动：

```bash
pm2 startup
```

执行命令输出的 `sudo ...` 命令后再保存：

```bash
pm2 save
```

## 5. Nginx 反向代理

```nginx
server {
    listen 80;
    server_name your-domain.example.com;

    client_max_body_size 256m;

    location / {
        proxy_pass http://127.0.0.1:3100;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 600s;
        proxy_send_timeout 600s;
    }
}
```

配置完成后使用 Certbot 或服务器面板签发 HTTPS 证书。

## 6. 后续更新

```bash
git fetch upstream main
git rebase upstream/main
./scripts/deploy-pm2.sh
```

如果部署的是自己的远程分支，则先拉取该分支，再运行部署脚本。

SQLite 数据保存在 `data/infinite-canvas.db`。更新代码前建议备份 `data/` 和 `.env`，不要把它们提交到 Git。

## 7. 免编译 Dist 部署

在 Windows 开发机执行：

```powershell
.\scripts\build-dist.ps1
```

构建结果位于 `deployment/infinite-canvas-dist`。将该目录内的全部文件上传并覆盖到服务器原项目目录，保留服务器已有 `.env`，然后执行：

```bash
chmod +x start.sh
./start.sh
```

发布包已经包含 Linux Go 后端和 Next.js standalone 前端，服务器不需要安装 Go、Bun，也不需要访问 GitHub，仅需 Node.js 和 PM2。默认构建 Linux x64；ARM64 服务器使用 `.\scripts\build-dist.ps1 -Arch arm64`。
