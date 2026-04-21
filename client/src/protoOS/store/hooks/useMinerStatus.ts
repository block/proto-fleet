import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import type { ErrorSource, MinerError } from "../types";
import useMinerStore from "../useMinerStore";

// =============================================================================
// Granular Hooks for Specific Data
// =============================================================================

export const useMiningStatus = () => {
  return useMinerStore((state) => state.minerStatus.miningStatus);
};

export const useMiningUptime = () => {
  return useMinerStore((state) => state.minerStatus.miningUptime);
};

export const useRebootUptime = () => {
  return useMinerStore((state) => state.minerStatus.rebootUptime);
};

export const useHwErrors = () => {
  return useMinerStore((state) => state.minerStatus.hwErrors);
};

export const useMiningStatusMessage = () => {
  return useMinerStore((state) => state.minerStatus.message);
};

// Derived flag hooks - compute from state
export const useIsWarmingUp = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    const miningUptimeS = state.minerStatus.miningUptime?.value || 0;
    const rebootUptimeS = state.minerStatus.rebootUptime?.value || 0;

    return /Uninitialized|PoweringOn/i.test(status) && (miningUptimeS < 60 || rebootUptimeS < 60);
  });
};

export const useIsSleeping = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    return /PoweringOff|Stopped/i.test(status);
  });
};

export const useIsMining = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    return /Mining/i.test(status);
  });
};

export const useIsAwake = () => {
  return useMinerStore((state) => {
    const status = state.minerStatus.miningStatus || "";
    return /PoweringOn|Mining|DegradedMining|NoPools|Error/i.test(status);
  });
};

export const useMinerErrors = () => {
  return useMinerStore(useShallow((state) => state.minerStatus.errors));
};

export const useOnboarded = () => {
  return useMinerStore((state) => state.minerStatus.onboarded);
};

export const usePasswordSet = () => {
  return useMinerStore((state) => state.minerStatus.passwordSet);
};

export const useDefaultPasswordActive = () => {
  return useMinerStore((state) => state.minerStatus.defaultPasswordActive);
};

/**
 * Hook to get system status data from minerStatus slice
 * Returns onboarded, passwordSet, and defaultPasswordActive status
 */
export const useSystemStatus = () =>
  useMinerStore(
    useShallow((state) => ({
      onboarded: state.minerStatus.onboarded,
      passwordSet: state.minerStatus.passwordSet,
      defaultPasswordActive: state.minerStatus.defaultPasswordActive,
    })),
  );

export const useWakeDialog = () => {
  return useMinerStore(useShallow((state) => state.ui.wakeDialog));
};

// =============================================================================
// Action Hooks
// =============================================================================

export const useSetMiningStatus = () => {
  return useMinerStore((state) => state.minerStatus.setMiningStatus);
};

export const useSetErrors = () => {
  return useMinerStore((state) => state.minerStatus.setErrors);
};

export const useSetOnboarded = () => {
  return useMinerStore((state) => state.minerStatus.setOnboarded);
};

export const useSetPasswordSet = () => {
  return useMinerStore((state) => state.minerStatus.setPasswordSet);
};

export const useSetDefaultPasswordActive = () => {
  return useMinerStore((state) => state.minerStatus.setDefaultPasswordActive);
};

/**
 * Hook to get system status setter actions
 */
export const useSetSystemStatus = () =>
  useMinerStore(
    useShallow((state) => ({
      setOnboarded: state.minerStatus.setOnboarded,
      setPasswordSet: state.minerStatus.setPasswordSet,
      setDefaultPasswordActive: state.minerStatus.setDefaultPasswordActive,
    })),
  );

export const useShowWakeDialog = () => {
  return useMinerStore((state) => state.ui.showWakeDialog);
};

export const useHideWakeDialog = () => {
  return useMinerStore((state) => state.ui.hideWakeDialog);
};

// =============================================================================
// Error Selector Hooks
// =============================================================================

/**
 * Returns errors grouped by component type
 * Groups errors by their source for UI display
 * Note: ASIC errors have already been transformed to HASHBOARD in the transformer
 * @returns Object with component types as keys and arrays of errors as values
 */
export const useGroupedErrors = () => {
  const allErrors = useMinerStore(useShallow((state) => state.minerStatus.errors.errors));

  return useMemo(() => {
    // Simple direct grouping by source
    // ASIC errors have already been transformed to HASHBOARD in the transformer
    // Pool-related errors are RIG source errors with pool-specific error codes
    const poolErrorCodes = ["NoPoolConfigured", "PoolConnectionLost"];

    return {
      hashboard: allErrors.filter((error) => error.source === "HASHBOARD"),
      psu: allErrors.filter((error) => error.source === "PSU"),
      fan: allErrors.filter((error) => error.source === "FAN"),
      pool: allErrors.filter((error) => error.source === "RIG" && poolErrorCodes.includes(error.errorCode)),
      system: allErrors.filter((error) => error.source === "RIG" && !poolErrorCodes.includes(error.errorCode)),
    };
  }, [allErrors]);
};

/**
 * Returns errors for a specific component
 * @param source - The error source (e.g., "FAN", "PSU")
 * @param slot - The 1-based component slot
 * @returns Array of errors for that specific component
 */
export const useErrorsByComponent = (source: ErrorSource, slot: number): MinerError[] => {
  return useMinerStore(
    useShallow((state) => {
      const allErrors = state.minerStatus.errors.errors;
      return allErrors.filter((error) => error.source === source && error.slot === slot);
    }),
  );
};

/**
 * Returns all errors from the store
 */
export const useErrors = (): MinerError[] => {
  return useMinerStore((state) => state.minerStatus.errors.errors);
};

/**
 * Returns whether the miner has any issues
 */
export const useHasIssues = (): boolean => {
  const errors = useMinerStore((state) => state.minerStatus.errors.errors);
  return errors.length > 0;
};
