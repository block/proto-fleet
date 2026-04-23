/// <reference types="vitest" />
/// <reference types="vite/client" />

import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";
import fs from "fs";
import path, { resolve } from "path";
import process from "process";
import { responsiveImagePlugin } from "./vitePlugins/responsiveImagePlugin";

// eslint-disable-next-line no-undef
const _dirname = __dirname;
const src = resolve(_dirname, "src");

const MODES = ["protoFleet", "protoOS"];

const createModeConfig = (mode) => {
  return {
    root: resolve(src, mode),
    publicDir: resolve(_dirname, "public"),
    build: {
      emptyOutDir: true,
      outDir: resolve(_dirname, `dist/${mode}`),
      rollupOptions: {
        input: `src/${mode}/index.html`,
        output: {
          manualChunks: (id: string) => {
            if (!id.includes("node_modules")) return;
            // Only force-bucket deps that we know ship in the entry. Everything
            // else falls through to rolldown so lazy-route deps (recharts,
            // dnd-kit, etc.) end up in their route chunk or a shared chunk
            // loaded alongside it — not preloaded with the entry.
            if (/[\\/]node_modules[\\/](react|react-dom|scheduler|use-sync-external-store)[\\/]/.test(id))
              return "vendor-react";
            if (/[\\/]node_modules[\\/](react-router|react-router-dom)[\\/]/.test(id)) return "vendor-router";
            if (/[\\/]node_modules[\\/](@bufbuild|@connectrpc)[\\/]/.test(id)) return "vendor-protobuf";
          },
        },
      },
    },
  };
};

const modes = MODES.reduce((acc, curr) => {
  return {
    [curr]: createModeConfig(curr),
    ...acc,
  };
}, {});

const defaultConfig = {
  root: src,
};

// build will build our html file to dist/{mode}/src/{mode}/index.html
// this will flatten the structure and bring the index.html down to the src
const moveHtmlFiles = (mode) => {
  return {
    name: "move-html-files",
    closeBundle() {
      const srcPath = resolve(_dirname, `dist/${mode}/src/${mode}/index.html`);
      const destPath = resolve(_dirname, `dist/${mode}/index.html`);

      if (fs.existsSync(srcPath)) {
        fs.mkdirSync(path.dirname(destPath), { recursive: true });
        fs.renameSync(srcPath, destPath);
      }

      // Clean up the unnecessary src directory
      const srcDir = resolve(_dirname, `dist/${mode}/src`);
      if (fs.existsSync(srcDir)) {
        fs.rmSync(srcDir, { recursive: true, force: true });
      }
    },
  };
};

const copyPublicDirectory = (mode, command) => {
  if (command !== "build") return;

  const destPath = resolve(src, `${mode}/public`);

  return {
    name: "copy-public-directory",

    // copy /public to src/{mode}/public
    buildStart() {
      const srcPath = resolve(_dirname, "public");
      const destPath = resolve(src, `${mode}/public`);

      if (fs.existsSync(srcPath)) {
        fs.cpSync(srcPath, destPath, { recursive: true });
      }
    },

    // remove directory from src after build
    closeBundle() {
      if (fs.existsSync(destPath)) {
        fs.rmSync(destPath, { recursive: true, force: true });
      }
    },
  };
};

// https://vitejs.dev/config/
export default defineConfig(({ mode, command }) => {
  if (!modes[mode] && command === "build" && process.env.BUILD_STORYBOOK != "1") {
    throw new Error("Build must be run with supported mode (eg. vite build --mode protoFleet)");
  }

  const env = loadEnv(mode, process.cwd(), "");
  let proxies;
  if (mode === "protoFleet") {
    const proxyUrl = env.FLEET_PROXY_URL || process.env.FLEET_PROXY_URL || "http://localhost:4000";
    proxies = {
      "/api-proxy": {
        target: proxyUrl,
        rewrite: (path: string) => path.replace(/^\/api-proxy/, ""),
        changeOrigin: true,
        secure: false,
      },
    };
  } else {
    // For ProtoOS: Use PROXY_URL from .env file
    const targetUrl = env.PROXY_URL || process.env.PROXY_URL;
    proxies = targetUrl
      ? {
          "/api/v1": {
            target: targetUrl,
            changeOrigin: true,
            secure: false,
          },
        }
      : {};

    // Log which proxy is being used for clarity

    if (targetUrl) {
      // eslint-disable-next-line no-console
      console.log(`[ProtoOS] Using direct miner connection at ${targetUrl}`);
    }
  }

  // eslint-disable-next-line no-console
  console.log(proxies);

  return {
    ...(modes[mode] || defaultConfig),
    base: "/",
    envDir: process.cwd(),
    plugins: [react(), responsiveImagePlugin(), moveHtmlFiles(mode), copyPublicDirectory(mode, command)],
    resolve: {
      alias: {
        "@": src,
        api: resolve(src, "api"),
        apiTypes: resolve(src, "api/types.ts"),
        icons: resolve(src, "assets/icons"),
      },
    },
    test: {
      globals: true,
      environment: "jsdom",
      setupFiles: ["tests/setup.ts"],
    },
    server: {
      proxy: proxies,
      historyApiFallback: true,
    },
    preview: {
      proxy: proxies,
    },
  };
});
