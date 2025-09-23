import { useCallback, useEffect, useMemo, useState } from "react";
import { HttpResponse, MiningTarget, MiningTargetResponse } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

type MiningTargetState = {
  value?: MiningTarget["power_target_watts"];
  default?: number;
  performanceMode?: MiningTarget["performance_mode"];
  bounds?: {
    min: number;
    max: number;
  };
  pending: boolean;
  error: string | null;
  listeners: Set<(state: Omit<MiningTargetState, "listeners">) => void>;
};

const miningTargetStore: MiningTargetState = {
  value: undefined,
  default: undefined,
  performanceMode: undefined,
  bounds: undefined,
  pending: false,
  error: null,
  listeners: new Set(),
};

const updateStore = (update: Partial<Omit<MiningTargetState, "listeners">>) => {
  Object.assign(miningTargetStore, update);

  const state = {
    value: miningTargetStore.value,
    default: miningTargetStore.default,
    performanceMode: miningTargetStore.performanceMode,
    bounds: miningTargetStore.bounds,
    pending: miningTargetStore.pending,
    error: miningTargetStore.error,
  };

  miningTargetStore.listeners.forEach((listener) => listener(state));
};

const fetchData = (api: any) => {
  if (!api) return;

  updateStore({ pending: true });

  api
    .getMiningTarget()
    .then((res: HttpResponse<MiningTargetResponse>) => {
      updateStore({
        value: res?.data.power_target_watts,
        default: res?.data.default_power_target_watts,
        performanceMode: res?.data.performance_mode,
        bounds: {
          min: res?.data.power_target_min_watts ?? 0,
          max: res?.data.power_target_max_watts ?? 0,
        },
        pending: false,
      });
    })
    .catch((err: any) => {
      updateStore({
        error: err?.error?.message ?? err,
        pending: false,
      });
    });
};

// Update data for all components
const sharedUpdateMiningTarget = (api: any, newTarget: MiningTarget) => {
  if (!api) return;

  updateStore({
    pending: true,
    error: null,
  });

  api
    .editMiningTarget(newTarget)
    .then((res: HttpResponse<MiningTargetResponse>) => {
      updateStore({
        value: res?.data.power_target_watts,
        performanceMode: res?.data.performance_mode,
        bounds: {
          min: res?.data.power_target_min_watts ?? 0,
          max: res?.data.power_target_max_watts ?? 0,
        },
        pending: false,
      });
    })
    .catch((err: any) => {
      updateStore({
        error: err?.error?.message ?? err,
        pending: false,
      });
    });
};

const useMiningTarget = () => {
  const { api } = useMinerHosting();
  const [localState, setLocalState] = useState({
    miningTarget: miningTargetStore.value,
    defaultTarget: miningTargetStore.default,
    performanceMode: miningTargetStore.performanceMode,
    bounds: miningTargetStore.bounds,
    pending: miningTargetStore.pending,
    error: miningTargetStore.error,
  });

  useEffect(() => {
    const listener = (state: Omit<MiningTargetState, "listeners">) => {
      setLocalState({
        miningTarget: state.value,
        defaultTarget: state.default,
        performanceMode: state.performanceMode,
        bounds: state.bounds,
        pending: state.pending,
        error: state.error,
      });
    };

    miningTargetStore.listeners.add(listener);

    return () => {
      miningTargetStore.listeners.delete(listener);
    };
  }, []);

  useEffect(() => {
    if (
      api &&
      miningTargetStore.value === undefined &&
      !miningTargetStore.pending
    ) {
      fetchData(api);
    }
  }, [api]);

  const updateMiningTarget = useCallback(
    (newTarget: MiningTarget) => {
      sharedUpdateMiningTarget(api, newTarget);
    },
    [api],
  );

  return useMemo(
    () => ({
      miningTarget: localState.miningTarget,
      defaultTarget: localState.defaultTarget,
      performanceMode: localState.performanceMode,
      bounds: localState.bounds,
      pending: localState.pending,
      error: localState.error,
      updateMiningTarget,
    }),
    [localState, updateMiningTarget],
  );
};

export { useMiningTarget };
