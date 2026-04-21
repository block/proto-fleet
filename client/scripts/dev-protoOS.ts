#!/usr/bin/env node

import { spawn } from "child_process";
import { config } from "dotenv";
import { fileURLToPath } from "url";
import { dirname, resolve } from "path";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Load .env file
config({ path: resolve(__dirname, "../.env") });

console.log("Starting ProtoOS...");

// Start vite normally
const vite = spawn("vite", ["--mode", "protoOS"], {
  stdio: "inherit",
  shell: true,
});

vite.on("exit", (code) => {
  process.exit(code || 0);
});
