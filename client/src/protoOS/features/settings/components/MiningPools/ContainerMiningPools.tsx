import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import MiningGroupConfiguration from "./MiningGroupConfiguration";
import MiningGroupOperations from "./MiningGroupOperations";
import ReadOnlyMiningPools from "./ReadOnlyMiningPools";
import { isAuthenticationError } from "@/protoOS/api/adapters/errors";
import { type ContainerPoolConfiguration, hasPendingPoolOperations } from "@/protoOS/api/adapters/poolConfiguration";
import type { MinerAdapter, SavedPoolSettings } from "@/protoOS/api/adapters/types";
import Pools from "@/protoOS/components/MiningPools/Pools";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import ContentHeader from "@/shared/components/ContentHeader";
import { emptyPoolInfo } from "@/shared/components/MiningPools/constants";
import PoolModal from "@/shared/components/MiningPools/PoolModal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import type { PoolInfo } from "@/shared/types/pool";

const POOL_READ_FAILED =
  "Could not reload pools or confirm mining-group mode. Shown settings may be stale. Reload before making further changes.";
const POOL_WRITE_FAILED = "The pool change did not complete. Review the reloaded configuration before trying again.";
const POOL_AUTH_REQUIRED = "Sign in to the controller, then reload pools. Pool changes were not retried.";
const readFailureMessage = (error: unknown) => {
  if (isAuthenticationError(error)) return POOL_AUTH_REQUIRED;
  return error instanceof Error ? error.message : POOL_READ_FAILED;
};

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
  const [snapshot, setSnapshot] = useState<{
    adapter: MinerAdapter;
    pools: SavedPoolSettings[];
    configuration: ContainerPoolConfiguration;
  }>();
  const currentSnapshot = snapshot?.adapter === adapter ? snapshot : undefined;
  const pools = currentSnapshot?.pools;
  const configuration = currentSnapshot?.configuration;
  const [error, setError] = useState<string>();
  const [unavailable, setUnavailable] = useState(false);
  const [busy, setBusy] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [revision, setRevision] = useState(0);
  const writing = useRef(false);
  const currentRead = useRef<AbortController | null>(null);
  const generation = useRef(0);
  const canEditContainerPools =
    configuration?.mode === "container" &&
    !configuration.suspended &&
    !hasPendingPoolOperations(configuration.pendingOperations);
  const controlsDisabled = busy || unavailable || refreshing;
  let subtitle = "Read the controller's pool configuration.";
  if (configuration?.mode === "groups") {
    subtitle = "Pools are managed by mining groups.";
  } else if (configuration?.mode === "container") {
    subtitle =
      "Add up to 3 pools in order of priority. If a pool fails or is removed, the container switches to the next available pool automatically.";
  }

  const refresh = useCallback(async () => {
    if (!adapter?.getPoolSettings) return;
    currentRead.current?.abort();
    const controller = new AbortController();
    currentRead.current = controller;
    const { signal } = controller;
    setRefreshing(true);
    try {
      if (!adapter.getContainerPoolConfiguration) throw new Error("Mining-group mode is unavailable.");
      const currentConfiguration = await adapter.getContainerPoolConfiguration({ signal });
      const current = currentConfiguration.mode !== "groups" ? await adapter.getPoolSettings({ signal }) : [];
      if (signal.aborted) return;
      setSnapshot({ adapter, pools: current, configuration: currentConfiguration });
      setUnavailable(false);
      setRevision((value) => value + 1);
    } catch (error) {
      if (signal.aborted) return;
      setUnavailable(true);
      if (isAuthenticationError(error)) throw error;
      throw Object.assign(new Error(POOL_READ_FAILED), { cause: error });
    } finally {
      if (currentRead.current === controller && !signal.aborted) setRefreshing(false);
    }
  }, [adapter]);

  useEffect(() => {
    generation.current += 1;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- start the initial device read and show its loading state
    void refresh().catch((error: unknown) => setError(readFailureMessage(error)));
    return () => {
      generation.current += 1;
      currentRead.current?.abort();
    };
  }, [refresh]);

  const reloadPools = () => {
    void refresh()
      .then(() => setError(undefined))
      .catch((error: unknown) => setError(readFailureMessage(error)));
  };

  const mutate = async (operation: () => Promise<void>) => {
    if (writing.current || unavailable || refreshing || !canEditContainerPools)
      throw new Error("Reload pools before making changes.");
    const started = generation.current;
    writing.current = true;
    setBusy(true);
    setError(undefined);
    let failure: unknown;
    let reloadFailure: unknown;
    try {
      await operation();
    } catch (error) {
      failure = error;
    }
    // CT1 can persist a change before its live reload fails. Always reconcile,
    // including partial reorders; never retry a create or roll back blindly.
    try {
      if (started !== generation.current) return;
      await refresh();
    } catch (error) {
      reloadFailure = error;
      failure ??= error;
    } finally {
      writing.current = false;
      setBusy(false);
    }
    if (failure) {
      const messages: string[] = [];
      if (failure !== reloadFailure) {
        const outcome = (failure as { writeOutcome?: string })?.writeOutcome;
        messages.push(outcome && failure instanceof Error ? failure.message : POOL_WRITE_FAILED);
      }
      if (reloadFailure)
        messages.push(isAuthenticationError(reloadFailure) ? POOL_READ_FAILED : readFailureMessage(reloadFailure));
      if (isAuthenticationError(failure) || isAuthenticationError(reloadFailure)) messages.push(POOL_AUTH_REQUIRED);
      setError(messages.join(" "));
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
        return;
      }
      if (!adapter?.reorderPoolSettings) throw new Error("Pool ordering unavailable");
      await adapter.reorderPoolSettings(ids);
    }).catch(() => {
      /* The page displays the reconciled mutation error. */
    });
  };

  return (
    <>
      <ContentHeader title="Pools" subtitle={subtitle} />
      {error ? (
        <div role="alert" className="mb-6 space-y-3">
          <Callout intent={intents.danger} prefixIcon={<Alert />} title={error} />
          {unavailable ? (
            <Button variant="secondary" text="Reload pools" disabled={refreshing} onClick={reloadPools} />
          ) : null}
        </div>
      ) : null}
      {!pools && !unavailable ? <ProgressCircular indeterminate /> : null}
      {configuration?.mode === "groups" && adapter ? (
        <div inert={unavailable || refreshing}>
          <MiningGroupConfiguration key={revision} configuration={configuration} adapter={adapter} />
        </div>
      ) : null}
      {configuration?.mode === "container" ? (
        <MiningGroupOperations operations={configuration.pendingOperations} />
      ) : null}
      {configuration?.mode === "unsupported" ? (
        <>
          <div className="mb-6" role="status">
            <Callout
              intent={intents.warning}
              prefixIcon={<Alert />}
              title="Pool editing is unavailable on this controller."
              subtitle="Mining-group support could not be confirmed. You can still view saved pools."
            />
          </div>
          <ReadOnlyMiningPools pools={pools ?? []} showHeader={false} />
        </>
      ) : null}
      {configuration?.mode === "container" && !canEditContainerPools ? (
        <>
          <div className="mb-6" role="status">
            <Callout
              intent={intents.warning}
              prefixIcon={<Alert />}
              title={configuration.suspended ? "Container mining is paused." : "A mining-group change is pending."}
              subtitle="Container-wide pool editing is unavailable. Reload pools to check again."
            />
          </div>
          <ReadOnlyMiningPools pools={pools ?? []} showHeader={false} />
        </>
      ) : null}
      {pools && canEditContainerPools ? (
        <div
          inert={controlsDisabled}
          aria-busy={busy || refreshing}
          className={controlsDisabled ? "opacity-50" : undefined}
        >
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
                onSave={(pool) =>
                  mutate(async () => {
                    if (!adapter?.savePoolSettings) throw new Error("Pool editing unavailable");
                    const priority = Math.max(-1, ...pools.map((pool) => pool.priority)) + 1;
                    await adapter.savePoolSettings({ ...pool, priority }, pool.id);
                  })
                }
              />
            )}
          />
        </div>
      ) : null}
      {!unavailable && pools ? (
        <Button
          className="mt-6"
          variant="secondary"
          text="Reload pools"
          disabled={busy || refreshing}
          onClick={reloadPools}
        />
      ) : null}
    </>
  );
};

export default ContainerMiningPools;
