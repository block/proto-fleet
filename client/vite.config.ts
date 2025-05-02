/// <reference types="vitest" />
/// <reference types="vite/client" />

import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv, splitVendorChunkPlugin } from "vite";
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
      const destPath = resolve(_dirname, `/dist/${mode}/index.html`);

      if (fs.existsSync(srcPath)) {
        fs.mkdirSync(path.dirname(destPath), { recursive: true });
        fs.renameSync(srcPath, destPath);
      }

      // Clean up the unnecessary src directory
      const srcDir = resolve(_dirname, `/dist/${mode}/src`);
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
  if (
    !modes[mode] &&
    command === "build" &&
    process.env.BUILD_STORYBOOK != "1"
  ) {
    throw new Error(
      "Build must be run with supported mode (eg. vite build --mode protoFleet)",
    );
  }

  let proxies = {};
  const env = loadEnv(mode, process.cwd(), "");
  if (mode === "protoFleet") {
    let proxyUrl = env.PROXY_URL || "http://localhost:4000";
    proxies = {
      "/fleetmanagement.v1.FleetManagementService": {
        target: proxyUrl,
      },
      "/auth.v1.AuthService": {
        target: proxyUrl,
      },
    };
  } else {
    proxies = env.PROXY_URL
      ? {
          "/api/v1": {
            target: env.PROXY_URL,
          },
        }
      : {};
  }

  // eslint-disable-next-line no-console
  console.log(proxies);

  return {
    ...(modes[mode] || defaultConfig),
    base: "/",
    envDir: process.cwd(),
    plugins: [
      react(),
      responsiveImagePlugin(),
      splitVendorChunkPlugin(),
      moveHtmlFiles(mode),
      copyPublicDirectory(mode, command),
    ],
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
    },
  };
});
