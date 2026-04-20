import type { ErrorSource } from "@/protoOS/store/types";

/**
 * Map of specific error codes to their title and subtitle messages
 * Used for displaying error-specific status titles in the UI
 */
export const singleErrorStatusTitles: Record<string, { title: string; subtitle: string }> = {
  // Fan errors
  "01:0001": {
    title: "Your miner's fan is running slowly",
    subtitle: "Repair now to prevent overheating",
  },
  "01:0002": {
    title: "Your miner's fan has stopped spinning",
    subtitle: "Repair now to prevent overheating",
  },
  "01:0003": {
    title: "Your miner's fan is in immersion mode",
    subtitle: "Check cooling configuration",
  },
  "01:0004": {
    title: "Your miner has insufficient cooling",
    subtitle: "Repair now to prevent overheating",
  },

  // PSU errors
  "00:0001": {
    title: "Your miner's power supply failed to start",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0002": {
    title: "Your miner's power supply has an overcurrent fault",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0003": {
    title: "Your miner's power supply has an overpower fault",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0004": {
    title: "Your miner's power supply has an overvoltage fault",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0005": {
    title: "Your miner's power supply has an undervoltage fault",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0006": {
    title: "Communication lost with power supply",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0007": {
    title: "Your miner's power supply has an input power error",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0008": {
    title: "Your miner's power supply is overheating",
    subtitle: "Repair now to prevent downtime",
  },
  "00:0012": {
    title: "Your miner's power supply is too cold",
    subtitle: "Check environment conditions",
  },
  "00:0013": {
    title: "Your miner's power supply is recovering",
    subtitle: "Hashboards in affected bay may be impacted",
  },

  // Hashboard errors
  "04:0006": {
    title: "Your miner's hashboard is overheating",
    subtitle: "Repair now to prevent reduced hashrate and board shutdowns",
  },
  "04:0007": {
    title: "Your miner's hashboard voltage is too high",
    subtitle: "Repair now to prevent overheating",
  },
  "04:0008": {
    title: "Your miner's hashboard voltage is too low",
    subtitle: "Repair now to prevent downtime",
  },
  "04:0009": {
    title: "Your miner's hashboard is drawing too much current",
    subtitle: "Repair now to prevent overheating",
  },
  "04:0011": {
    title: "Your miner's hashboard has lost power",
    subtitle: "Repair now to prevent downtime",
  },
  "04:0015": {
    title: "Your miner's hashboard has lost USB connection",
    subtitle: "Repair now to prevent downtime",
  },
  "04:0017": {
    title: "Your miner's hashboard is recovering",
    subtitle: "Monitor for continued issues",
  },

  // ASIC errors
  "04:0001": {
    title: "Your miner's ASICs are overheating",
    subtitle: "Repair now to prevent reduced hashrate and board shutdowns",
  },
  "04:0002": {
    title: "Your miner's ASIC voltage is excessive",
    subtitle: "Repair now to prevent reduced hashrate and board shutdowns",
  },
  "04:0003": {
    title: "Your miner's ASIC voltage is too low",
    subtitle: "Repair now to prevent reduced hashrate and board shutdowns",
  },
  "04:0004": {
    title: "Your miner's ASICs have ECC errors",
    subtitle: "Repair now to prevent reduced hashrate and board shutdowns",
  },
  "04:0005": {
    title: "Your miner's ASICs are malfunctioning",
    subtitle: "Repair now to prevent reduced hashrate and board shutdowns",
  },
  "04:0014": {
    title: "Your miner's ASICs are too cold",
    subtitle: "Check environment conditions to prevent performance issues",
  },

  // Pool errors
  "03:0006": {
    title: "Your miner has pool connection issues",
    subtitle: "Check pool configuration",
  },

  // System errors
  "03:0015": {
    title: "Incompatible hashboards in the same bay",
    subtitle: "Repair now to prevent downtime",
  },
};

/**
 * Generic fallback messages for when specific error code not found
 * Used when displaying errors without specific error code mappings
 */
export const genericStatusTitles: Record<ErrorSource, { title: string; subtitle: string }> = {
  RIG: {
    title: "Your miner has a system issue",
    subtitle: "Check diagnostics for details",
  },
  FAN: {
    title: "Your miner has a fan issue",
    subtitle: "Repair now to prevent overheating",
  },
  PSU: {
    title: "Your miner has a power supply issue",
    subtitle: "Repair now to prevent downtime",
  },
  HASHBOARD: {
    title: "Your miner has a hashboard issue",
    subtitle: "Repair now to prevent reduced hashrate",
  },
};

// Component display names are now handled directly in useComponentDisplayName hook
