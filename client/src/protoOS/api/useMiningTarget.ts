import { useCallback, useEffect, useMemo, useState } from "react";
import { MiningTarget } from "./types";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";

type MiningTargetState = {
  value?: MiningTarget["power_target_watts"];
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
  bounds: undefined,
  pending: false,
  error: null,
  listeners: new Set(),
};

const updateStore = (update: Partial<Omit<MiningTargetState, "listeners">>) => {
  Object.assign(miningTargetStore, update);

  const state = {
    value: miningTargetStore.value,
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
    .then((res: any) => {
      updateStore({
        value: res?.data["power_target_watts"],
        bounds: {
          min: res?.data["power_target_min_watts"],
          max: res?.data["power_target_max_watts"],
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
const sharedUpdateMiningTarget = (
  api: any,
  newTarget: MiningTarget["power_target_watts"],
) => {
  if (!api) return;

  updateStore({
    pending: true,
    error: null,
  });

  api
    .editMiningTarget({ power_target_watts: newTarget })
    .then((res: any) => {
      updateStore({
        value: res?.data["power_target_watts"],
        bounds: {
          min: res?.data["power_target_watts_min"],
          max: res?.data["power_target_watts_max"],
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
    bounds: miningTargetStore.bounds,
    pending: miningTargetStore.pending,
    error: miningTargetStore.error,
  });

  useEffect(() => {
    const listener = (state: Omit<MiningTargetState, "listeners">) => {
      setLocalState({
        miningTarget: state.value,
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
    (newTarget: MiningTarget["power_target_watts"]) => {
      sharedUpdateMiningTarget(api, newTarget);
    },
    [api],
  );

  return useMemo(
    () => ({
      miningTarget: localState.miningTarget,
      bounds: localState.bounds,
      pending: localState.pending,
      error: localState.error,
      updateMiningTarget,
    }),
    [localState, updateMiningTarget],
  );
};

export { useMiningTarget };
