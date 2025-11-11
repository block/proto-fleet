import { spawn, ChildProcess } from 'child_process';
import { fileURLToPath } from 'url';
import { dirname, resolve } from 'path';
import fs from 'fs';
import type { MinefieldConfig, MinefieldProcess } from './types';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

export async function startMinefield(config: MinefieldConfig): Promise<MinefieldProcess> {
  const {
    proxyUrl,
    minefieldUrl = 'http://localhost:7070',
    verbose = false,
    autoBuild = true
  } = config;

  // Extract port from MINEFIELD_URL
  let minefieldPort: number;
  try {
    const url = new URL(minefieldUrl);
    minefieldPort = parseInt(url.port || '7070');
  } catch {
    minefieldPort = 7070;
  }
  const controlPort = minefieldPort + 1;

  // Paths relative to minefield package root
  const minefieldRoot = resolve(__dirname, '../..');
  const minefieldBinary = resolve(minefieldRoot, 'bin/minefield');
  const minefieldWebDist = resolve(minefieldRoot, 'web/dist');

  // Check what needs to be built
  const needsBinary = !fs.existsSync(minefieldBinary);
  const needsWebUI = !fs.existsSync(minefieldWebDist);

  if (autoBuild && (needsBinary || needsWebUI)) {
    const buildSteps = [];
    if (needsBinary) {
      console.log('Minefield binary not found, building...');
      buildSteps.push('build');
    }
    if (needsWebUI) {
      console.log('Minefield web UI not found, building...');
      buildSteps.push('web-build');
    }

    console.log('\nBuilding minefield components...');

    // Build using just commands
    for (const step of buildSteps) {
      await runJustCommand(minefieldRoot, step);
    }
    console.log('Minefield components built successfully!\n');
  }

  // Start minefield proxy
  console.log('Starting Minefield proxy...');
  console.log(`  Proxy port: ${minefieldPort}`);
  console.log(`  Control port: ${controlPort}`);
  console.log(`  Target miner: ${proxyUrl}`);

  const args = [
    '-proxy', `:${minefieldPort}`,
    '-control', `:${controlPort}`,
    '-target', proxyUrl
  ];

  if (verbose) {
    args.push('-verbose');
  }

  const process = spawn(minefieldBinary, args, {
    cwd: minefieldRoot,  // Run from minefield directory so it finds ./web/dist
    stdio: 'pipe',
    detached: false
  });

  // Log output with prefix
  process.stdout?.on('data', (data) => {
    process.stdout.write(`[minefield] ${data}`);
  });

  process.stderr?.on('data', (data) => {
    process.stderr.write(`[minefield] ${data}`);
  });

  process.on('error', (err) => {
    console.error('Failed to start minefield:', err);
    throw err;
  });

  // Wait a moment for minefield to start
  await new Promise(resolve => setTimeout(resolve, 1000));

  return {
    kill: () => process.kill(),
    port: minefieldPort,
    controlPort
  };
}

async function runJustCommand(cwd: string, command: string): Promise<void> {
  return new Promise((resolve, reject) => {
    console.log(`Running: just ${command}`);
    const proc = spawn('just', [command], {
      cwd,
      stdio: 'inherit',
      shell: true
    });

    proc.on('error', (err: any) => {
      if (err.code === 'ENOENT') {
        console.error('ERROR: "just" command not found. Please install it first.');
        console.error('Visit: https://github.com/casey/just');
      } else {
        console.error(`Error running ${command}:`, err);
      }
      reject(err);
    });

    proc.on('close', (code) => {
      if (code !== 0) {
        reject(new Error(`Failed to run ${command}`));
      } else {
        resolve();
      }
    });
  });
}