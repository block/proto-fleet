#!/usr/bin/env node

import { spawn, ChildProcess } from "child_process";
import { config } from "dotenv";
import { fileURLToPath } from "url";
import { dirname, resolve } from "path";
import { startMinefield } from "@proto-fleet/minefield/server";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

// Load .env file
config({ path: resolve(__dirname, "../.env") });

// Check for --minefield flag
const useMinefield = process.argv.includes("--minefield");

if (!useMinefield) {
  console.log("Starting ProtoOS without minefield...");

  // Just start vite normally
  const vite = spawn("vite", ["--mode", "protoOS"], {
    stdio: "inherit",
    shell: true,
  });

  vite.on("exit", (code) => {
    process.exit(code || 0);
  });
} else {
  console.log("Starting ProtoOS with minefield...");

  const PROXY_URL = process.env.PROXY_URL;

  if (!PROXY_URL) {
    console.error(
      "ERROR: --minefield flag is set but PROXY_URL is not defined in .env",
    );
    console.error("Please set PROXY_URL to your target miner URL");
    process.exit(1);
  }

  const MINEFIELD_URL = process.env.MINEFIELD_URL || "http://localhost:7070";

  // Start minefield using the package
  startMinefield({
    proxyUrl: PROXY_URL,
    minefieldUrl: MINEFIELD_URL,
    verbose: true,
    autoBuild: true,
  })
    .then((minefield) => {
      console.log(
        `\nMinefield control UI available at http://localhost:${minefield.controlPort}\n`,
      );
      console.log("Starting ProtoOS dev server...");
      console.log(`ProtoOS will connect to minefield at ${MINEFIELD_URL}\n`);

      // Start vite with MINEFIELD_URL set and VITE_MINEFIELD_ACTIVE flag
      // We need to set these before spawning vite for them to be recognized
      process.env.VITE_MINEFIELD_URL = MINEFIELD_URL;

      const vite = spawn("vite", ["--mode", "protoOS"], {
        stdio: "inherit",
        shell: true,
        env: {
          ...process.env,
        },
      });

      // Handle exit
      const cleanup = () => {
        console.log("\nShutting down...");
        minefield.kill();
        vite.kill();
        process.exit(0);
      };

      process.on("SIGINT", cleanup);
      process.on("SIGTERM", cleanup);

      vite.on("exit", (code) => {
        minefield.kill();
        process.exit(code || 0);
      });
    })
    .catch((err) => {
      console.error("Failed to start minefield:", err);
      process.exit(1);
    });
}
