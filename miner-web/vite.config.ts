/// <reference types="vitest" />
/// <reference types="vite/client" />

import react from "@vitejs/plugin-react";
import { defineConfig, splitVendorChunkPlugin } from "vite";
import { resolve } from "path";
import process from "process";

// eslint-disable-next-line no-undef
const root = resolve(__dirname, "src");

// https://vitejs.dev/config/
export default defineConfig(() => {
  const apiServers = {
    "swagger": "https://virtserver.swaggerhub.com/proto-team/mining-development-kit-api/1.0.0",
    "local": "http://127.0.0.1:8080",
  };

  process.env.VITE_API_BASE_URL = process.env.API_SERVER ? apiServers[process.env.API_SERVER] : "";

  return {
    plugins: [react(), splitVendorChunkPlugin()],
    resolve: {
      alias: {
        api: resolve(root, "api"),
        apiTypes: resolve(root, "api/types.ts"),
        common: resolve(root, "common"),
        components: resolve(root, "components"),
        icons: resolve(root, "assets/icons"),
        pages: resolve(root, "pages"),
      },
    },
    test: {
      globals: true,
      environment: "jsdom",
      setupFiles: ["src/common/tests/setup.ts"],
    },
  };
});
