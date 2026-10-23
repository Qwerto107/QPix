import { execFileSync } from "node:child_process";
import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

let commitHash = process.env.QPIX_COMMIT_HASH || process.env.GITHUB_SHA || "";
if (!commitHash) {
  try {
    commitHash = execFileSync("git", ["rev-parse", "HEAD"], {
      cwd: new URL("..", import.meta.url),
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    // 无 Git 元数据的源码包显示 unknown，避免把未知构建误标成某次提交。
    commitHash = "unknown";
  }
}
commitHash = /^[0-9a-f]{8,64}$/i.test(commitHash.trim())
  ? commitHash.trim().slice(0, 8).toLowerCase()
  : "unknown";

export default defineConfig({
  define: { __COMMIT_HASH__: JSON.stringify(commitHash) },
  plugins: [vue()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: "http://127.0.0.1:8080" },
      "/i": { target: "http://127.0.0.1:8080" },
      "/si": { target: "http://127.0.0.1:8080" },
    },
  },
});
