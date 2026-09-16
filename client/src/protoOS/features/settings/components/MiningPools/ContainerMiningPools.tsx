import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { SavedPoolSettings } from "@/protoOS/api/adapters/types";
import Pools from "@/protoOS/components/MiningPools/Pools";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import Button from "@/shared/components/Button";
import ContentHeader from "@/shared/components/ContentHeader";
import { emptyPoolInfo } from "@/shared/components/MiningPools/constants";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import type { PoolInfo } from "@/shared/types/pool";

// CT1's ParsePoolURL also accepts host:port shorthand and DATUM gateways.
const validateContainerPoolURL = (value: string): string | undefined => {
  const url = value.trim();
  try {
    const parsed = new URL(url.includes("://") ? url : `stratum+tcp://${url}`);
    if (!["stratum+tcp:", "stratum+ssl:", "tcp:", "ssl:", "datum:"].includes(parsed.protocol)) {
      return "Use stratum+tcp://, stratum+ssl://, tcp://, ssl://, datum://, or host:port.";
    }
    if (parsed.hostname) return undefined;
  } catch {
    // Invalid addresses cannot be submitted to the container.
  }
  return "Enter a valid pool host or IP address.";
};

const ContainerMiningPools = () => {
  const { adapter } = useMinerHosting();
  const [pools, setPools] = useState<SavedPoolSettings[]>();
  const [error, setError] = useState<string>();
  const [unavailable, setUnavailable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0);
  const writing = useRef(false);

  const refresh = useCallback(
    async (signal?: AbortSignal) => {
      if (!adapter?.getPoolSettings) return;
      try {
        const current = await adapter.getPoolSettings({ signal });
        if (signal?.aborted) return;
        setPools(current);
        setUnavailable(false);
        setRevision((value) => value + 1);
      } catch {
        if (signal?.aborted) return;
        setUnavailable(true);
        throw new Error("Could not reload pools. Reload before making further changes.");
      }
    },
    [adapter],
  );

  useEffect(() => {
    const controller = new AbortController();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- refresh writes state only after the asynchronous device read completes
    void refresh(controller.signal).catch((error: Error) => setError(error.message));
    return () => controller.abort();
  }, [refresh]);

  const mutate = async (operation: () => Promise<void>) => {
    if (writing.current || unavailable) throw new Error("Wait for pools to finish loading");
    writing.current = true;
    setBusy(true);
    setError(undefined);
    let failure: Error | undefined;
    try {
      await operation();
    } catch {
      failure = new Error("The pool change did not complete. Review the reloaded configuration before trying again.");
    }
    // CT1 can persist a change before its live reload fails. Always reconcile,
    // including partial reorders; never retry a create or roll back blindly.
    try {
      await refresh();
    } catch (error) {
      failure = error as Error;
    } finally {
      writing.current = false;
      setBusy(false);
    }
    if (failure) {
      setError(failure.message);
      throw failure;
    }
  };

  const formPools = useMemo(() => {
    const result: PoolInfo[] = [...(pools ?? [])];
    while (result.length < 3) result.push({ ...emptyPoolInfo, priority: result.length });
    return result;
  }, [pools]);

  const changeOrderOrDelete = (next: PoolInfo[], options?: { isDelete?: boolean }) => {
    const ids = next.flatMap((pool) => (pool.id ? [pool.id] : []));
    void mutate(async () => {
      if (options?.isDelete) {
        const removed = pools?.find((pool) => !ids.includes(pool.id));
        if (!removed || !adapter?.deletePoolSettings) throw new Error("Pool unavailable");
        await adapter.deletePoolSettings(removed.id);
      } else {
        if (!adapter?.reorderPoolSettings) throw new Error("Pool ordering unavailable");
        await adapter.reorderPoolSettings(ids);
      }
    }).catch(() => {
      /* The page displays the reconciled mutation error. */
    });
  };

  return (
    <>
      <ContentHeader
        title="Pools"
        subtitle="Add up to 3 pools in order of priority. If a pool fails or is removed, the container switches to the next available pool automatically."
      />
      {error ? (
        <div role="alert" className="mb-6 space-y-3">
          <p>{error}</p>
          {unavailable ? (
            <Button
              variant="secondary"
              text="Reload pools"
              onClick={() => {
                void refresh()
                  .then(() => setError(undefined))
                  .catch((error: Error) => setError(error.message));
              }}
            />
          ) : null}
        </div>
      ) : null}
      {!pools && !unavailable ? <ProgressCircular indeterminate /> : null}
      {pools ? (
        <div inert={busy || unavailable} aria-busy={busy} className={busy || unavailable ? "opacity-50" : undefined}>
          <Pools
            key={revision}
            pools={formPools}
            onChangePools={changeOrderOrDelete}
            showDeleteToast={false}
            renderModal={(props) => (
              <PoolModal
                {...props}
                onChangePools={() => {}}
                isTestingConnection={false}
                testConnection={() => {}}
                hideConnectionTest
                validateURL={validateContainerPoolURL}
                description={
                  props.mode === "edit"
                    ? "Leave the password blank to keep the saved password."
                    : "Add a pool for this container."
                }
                onSave={async (pool) => {
                  await mutate(async () => {
                    if (!adapter?.savePoolSettings) throw new Error("Pool editing unavailable");
                    const priority = Math.max(-1, ...(pools ?? []).map((pool) => pool.priority)) + 1;
                    await adapter.savePoolSettings({ ...pool, priority }, pool.id);
                  });
                }}
              />
            )}
          />
        </div>
      ) : null}
    </>
  );
};

export default ContainerMiningPools;
