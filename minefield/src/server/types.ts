export interface MinefieldConfig {
  proxyUrl: string;          // Target miner URL
  minefieldUrl?: string;     // Minefield proxy URL (default: http://localhost:7070)
  verbose?: boolean;         // Enable verbose logging
  autoBuild?: boolean;       // Auto-build if binaries missing (default: true)
}

export interface MinefieldProcess {
  kill: () => void;
  port: number;
  controlPort: number;
}