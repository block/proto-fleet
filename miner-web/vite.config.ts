/// <reference types="vitest" />
/// <reference types="vite/client" />

import react from "@vitejs/plugin-react";
import { defineConfig, splitVendorChunkPlugin } from "vite";
import { resolve } from "path";

// eslint-disable-next-line no-undef
const root = resolve(__dirname, "src");

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), splitVendorChunkPlugin()],
  resolve: {
    alias: {
      api: resolve(root, "api"),
      apiTypes: resolve(root, "api/types.ts"),
      common: resolve(root, "common"),
      components: resolve(root, "components"),
      icons: resolve(root, "assets/icons/"),
      pages: resolve(root, "pages"),
    },
  },
  server: {
    proxy: {
      // miner-api-server http://127.0.0.1:8080
      // "/api": "https://virtserver.swaggerhub.com/KSHITIZ_1/MDK-API/1.0.0",
      "/api": "http://127.0.0.1:8080",
    },
  },
  test: {
    globals: true,
    environment: "jsdom",
  },
});
