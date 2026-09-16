import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { type ComponentRef, type MinerError as NormalizedMinerError, ROOT_MODULE_KEY } from "../contract";
import type { ErrorSource, MinerError } from "../types";
import useMinerStore from "../useMinerStore";
import { resolveComponentAddress } from "../utils/componentSelection";

const selectOperationalStatus = (state: ReturnType<typeof useMinerStore.getState>) =>
  state.minerStatus.operationalByOwner.get(ROOT_MODULE_KEY);

const componentSlot = (error: NormalizedMinerError) => {
  if (error.source?.slot !== undefined) return error.source.slot;
  const match = error.component?.id.toString().match(/(?:slot:|:)(\d+)$/);
  return match ? Number(match[1]) : undefined;
};

const errorSource = (error: NormalizedMinerError): ErrorSource => {
  const kind = error.source?.kind ?? error.component?.kind ?? "rig";
  return kind === "hashboard" ? "HASHBOARD" : kind === "psu" ? "PSU" : kind === "fan" ? "FAN" : "RIG";
};

const toLegacyError = (error: NormalizedMinerError): MinerError => ({
  errorCode: error.code,
  timestamp: error.timestampMs === undefined ? undefined : error.timestampMs / 1_000,
  source: errorSource(error),
  slot: componentSlot(error),
  message: error.message,
});

// =============================================================================
// Granular Hooks for Specific Data
// =============================================================================

export const useMiningStatus = () => {
  return useMinerStore((state) => selectOperationalStatus(state)?.miningStatus);
};

export const useMiningUptime = () => {
  return useMinerStore((state) => selectOperationalStatus(state)?.miningUptime);
};

export const useRebootUptime = () => {
  return useMinerStore((state) => selectOperationalStatus(state)?.rebootUptime);
};

export const useHwErrors = () => {
  return useMinerStore((state) => selectOperationalStatus(state)?.hardwareErrorCount);
};

export const useMiningStatusMessage = () => {
  return useMinerStore((state) => selectOperationalStatus(state)?.message);
};

// Derived flag hooks - compute from state
export const useIsWarmingUp = () => {
  return useMinerStore((state) => {
    const operational = selectOperationalStatus(state);
    const status = operational?.miningStatus;
    const miningUptimeS = operational?.miningUptime?.value || 0;
    const rebootUptimeS = operational?.rebootUptime?.value || 0;

    return (status === "uninitialized" || status === "poweringOn") && (miningUptimeS < 60 || rebootUptimeS < 60);
  });
};

export const useIsSleeping = () => {
  return useMinerStore((state) => {
    const status = selectOperationalStatus(state)?.miningStatus;
    return status === "poweringOff" || status === "stopped";
  });
};

export const useIsMining = () => {
  return useMinerStore((state) => {
    const status = selectOperationalStatus(state)?.miningStatus;
    return status === "mining" || status === "degradedMining";
  });
};

export const useIsAwake = () => {
  return useMinerStore((state) => {
    const status = selectOperationalStatus(state)?.miningStatus;
    // Curtailed counts as awake: the rig is powered and responsive, mining is
    // just paused by the curtailment service. Offering "Wake up" would be
    // immediately overridden by curtailment, while "Sleep" (manual stop) is
    // meaningful under the respect_manual_stop restore policy.
    return ["poweringOn", "mining", "degradedMining", "curtailed", "noPools", "error"].includes(status ?? "");
  });
};

export const useMinerErrors = () => {
  const errors = useErrors();
  return useMemo(() => ({ errors }), [errors]);
};

export const useOnboarded = () => {
  return useMinerStore((state) => state.minerStatus.lifecycle.onboarded);
};

export const usePasswordSet = () => {
  return useMinerStore((state) => state.minerStatus.lifecycle.passwordSet);
};

export const useDefaultPasswordActive = () => {
  return useMinerStore((state) => state.minerStatus.lifecycle.defaultPasswordActive);
};

/**
 * Hook to get system status data from minerStatus slice
 * Returns onboarded, passwordSet, and defaultPasswordActive status
 */
export const useSystemStatus = () =>
  useMinerStore(
    useShallow((state) => ({
      onboarded: state.minerStatus.lifecycle.onboarded,
      passwordSet: state.minerStatus.lifecycle.passwordSet,
      defaultPasswordActive: state.minerStatus.lifecycle.defaultPasswordActive,
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
  const allErrors = useErrors();

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
  const errors = useErrors();
  return useMemo(
    () => errors.filter((error) => error.source === source && error.slot === slot),
    [errors, slot, source],
  );
};

/**
 * Returns all errors from the store
 */
export const useErrors = (): MinerError[] => {
  const errors = useDetailedErrors();
  return useMemo(() => errors.map(toLegacyError), [errors]);
};

/**
 * Returns whether the miner has any issues
 */
export const useHasIssues = (): boolean => {
  return useMinerStore((state) => [...state.minerStatus.errorsByOwner.values()].some((errors) => errors.length > 0));
};

/** Current detailed errors across every owner; preserve canonical component refs. */
export const useDetailedErrors = () => {
  const byOwner = useMinerStore((state) => state.minerStatus.errorsByOwner);
  const hardware = useMinerStore((state) => state.hardware);
  return useMemo(
    () =>
      [...byOwner.values()].flat().map((error) => {
        if (error.component || !error.source) return error;
        const component = resolveComponentAddress(
          {
            source: errorSource(error),
            slot: error.source.slot,
            moduleId: error.owner.kind === "module" ? error.owner.id : undefined,
          },
          hardware,
        );
        return component ? { ...error, component } : error;
      }),
    [byOwner, hardware],
  );
};

export const useErrorsByRef = (component: ComponentRef | undefined) => {
  const errors = useDetailedErrors();
  return useMemo(
    () =>
      component
        ? errors.filter((error) => error.component?.kind === component.kind && error.component.id === component.id)
        : [],
    [errors, component],
  );
};
