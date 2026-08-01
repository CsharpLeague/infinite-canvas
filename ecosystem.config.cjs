const path = require("node:path");
const fs = require("node:fs");

const root = __dirname;
const envFile = path.join(root, ".env");

if (fs.existsSync(envFile) && typeof process.loadEnvFile === "function") {
    process.loadEnvFile(envFile);
}

module.exports = {
    apps: [
        {
            name: "infinite-canvas-api",
            cwd: root,
            script: path.join(root, "bin", "infinite-canvas-api"),
            interpreter: "none",
            autorestart: true,
            max_restarts: 10,
            restart_delay: 3000,
            kill_timeout: 10000,
            time: true,
            env: {
                PORT: process.env.PORT || "8080",
            },
        },
        {
            name: "infinite-canvas-web",
            cwd: path.join(root, "web", ".next", "standalone"),
            script: "server.js",
            interpreter: "node",
            autorestart: true,
            max_restarts: 10,
            restart_delay: 3000,
            kill_timeout: 10000,
            time: true,
            env: {
                NODE_ENV: "production",
                HOSTNAME: "0.0.0.0",
                PORT: process.env.WEB_PORT || "3100",
                API_BASE_URL: process.env.API_BASE_URL || `http://127.0.0.1:${process.env.PORT || "8080"}`,
            },
        },
    ],
};
