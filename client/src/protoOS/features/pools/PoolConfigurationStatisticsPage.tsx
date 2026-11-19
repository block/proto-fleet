import { type ReactNode, useState } from "react";
import { useSearchParams } from "react-router-dom";
import PoolStatisticsView from "./PoolStatisticsView";
import { configurationShares, usePoolConfigurationStatistics } from "./usePoolConfigurationStatistics";
import { type PoolDuration, poolDurations } from "./usePoolStatistics";
import type {
  PoolConfigurationReadings,
  PoolConfigurationStatisticsService,
} from "@/protoOS/api/adapters/poolStatistics";
import { navigationItems } from "@/protoOS/components/NavigationMenu/constants";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { Alert } from "@/shared/assets/icons";
import Breadcrumb from "@/shared/components/Breadcrumb";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import DurationSelector from "@/shared/components/DurationSelector";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import { PageControls } from "@/shared/components/PageControls";
import ProgressCircular from "@/shared/components/ProgressCircular";
import SettingsEmptyState from "@/shared/components/SettingsEmptyState";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const compactNumber = new Intl.NumberFormat("en", { notation: "compact", maximumFractionDigits: 1 });
const number = (value?: number) => (value === undefined ? "—" : compactNumber.format(value));
export default function PoolConfigurationStatisticsPage({
  service,
  legacy,
}: {
  service: PoolConfigurationStatisticsService;
  legacy: ReactNode;
}) {
  const [duration, setDuration] = useState<PoolDuration>("24h");
  const [page, setPage] = useState(0);
  const [search, setSearch] = useSearchParams();
  const selectedId = search.get("configuration") ?? undefined;
  const select = (id?: string) =>
    setSearch((current) => {
      const next = new URLSearchParams(current);
      if (id) next.set("configuration", id);
      else next.delete("configuration");
      return next;
    });
  const { isPhone } = useWindowDimensions();
  const { minerRoot } = useMinerHosting();
  const summarySearch = new URLSearchParams(search);
  summarySearch.delete("configuration");
  const poolsHref = `${minerRoot}/${navigationItems.pools}${summarySearch.size ? `?${summarySearch}` : ""}`;
  const configureHref = `${minerRoot}/${navigationItems.miningPools}`;
  const state = usePoolConfigurationStatistics(service, duration, page, selectedId);
  if (state.enabled === false && !state.error) return legacy;
  const title = selectedId ? (state.selected?.name ?? "Pool configuration") : "Pools";
  const selected = state.readings?.find((configuration) => configuration.id === selectedId);
  const rows = (state.readings ?? []).map((configuration) => ({
    ...configuration,
    shares: configurationShares(configuration.pools, state.history),
  }));
  type Item = (typeof rows)[number];
  const activePool = (configuration: PoolConfigurationReadings) => {
    const active = configuration.pools.filter((pool) => pool.status === "Active" || pool.status === "Alive");
    if (active.length) return active.map((pool) => pool.url).join(", ");
    return configuration.stale || configuration.error || configuration.pools.some((pool) => pool.status === "Unknown")
      ? "Unknown"
      : "No active pool";
  };
  const loading =
    state.enabled === undefined ||
    (!state.readings && state.configurations.length > 0 && (!selectedId || Boolean(state.selected)));
  return (
    <div className="min-w-0 pb-6">
      <div className="flex flex-col gap-6 px-4 pt-6 tablet:px-10 tablet:pt-10">
        <div className="flex flex-col gap-3">
          {selectedId ? <Breadcrumb segments={[{ label: "Pools", to: poolsHref }, { label: title }]} /> : null}
          <Header title={title} titleSize="text-heading-300" />
        </div>
        {selected?.observedBoards !== undefined && selected.observedBoards < selected.assignedCount ? (
          <Callout
            intent="warning"
            prefixIcon={<Alert />}
            title={`Connection status covers ${selected.observedBoards} of ${selected.assignedCount} hashboards.`}
          />
        ) : null}
        {state.error ? (
          <div role="alert" className="flex flex-col items-start gap-3">
            <Callout intent="danger" prefixIcon={<Alert />} title={state.error} />
            <Button variant="secondary" text="Retry" onClick={state.refresh} />
          </div>
        ) : null}
      </div>
      {selectedId && selected ? (
        <PoolStatisticsView
          pools={selected.stale ? undefined : selected.pools}
          history={state.history}
          pending={state.historyPending}
          duration={duration}
          onDurationChange={setDuration}
          configureHref={configureHref}
          usernameLabel="Username"
          error={selected.stale ? "Configuration changed. Waiting for current statistics." : selected.error}
        />
      ) : (
        <div className="px-4 pt-6 tablet:px-10">
          <div className="mb-6 flex flex-wrap items-center justify-between gap-4">
            <DurationSelector duration={duration} durations={poolDurations} onSelect={setDuration} />
            <Button text="Configure pools" variant="secondary" size="compact" to={configureHref} />
          </div>
          {loading && !state.error ? (
            <div className="flex justify-center py-10" role="status">
              <ProgressCircular indeterminate />
              <span className="sr-only">Loading configuration statistics…</span>
            </div>
          ) : null}
          {selectedId && state.enabled && !state.selected ? (
            <SettingsEmptyState
              title="This configuration is no longer in use"
              description="Return to all configurations to view current pool activity."
            />
          ) : null}
          {!selectedId && state.enabled && state.configurations.length === 0 ? (
            <SettingsEmptyState
              title="No configurations in use"
              description="Assign a pool configuration to modules or hashboards to see statistics here."
            />
          ) : null}
          {!selectedId && state.readings ? (
            <>
              <List<Item, string, "name" | "active" | "assigned" | "rate" | "accepted" | "rejected">
                items={rows}
                itemKey="id"
                itemSelectable={false}
                onRowClick={(item) => select(item.id)}
                stickyFirstColumn={false}
                applyColumnWidthsToCells
                tableClassName="mb-0"
                activeCols={
                  isPhone ? ["name", "active", "rate"] : ["name", "active", "assigned", "rate", "accepted", "rejected"]
                }
                colTitles={{
                  name: "Configuration",
                  active: "Active pool",
                  assigned: "Assigned hashboards",
                  rate: isPhone ? "Acceptance" : "Acceptance rate",
                  accepted: "Accepted",
                  rejected: "Rejected",
                }}
                colConfig={{
                  name: {
                    width: "w-1/5 min-w-40 phone:min-w-24",
                    allowWrap: true,
                    component: (item) => (
                      <div>
                        <span className="font-medium">{item.name}</span>
                        {isPhone ? (
                          <p className="text-200 text-text-primary-50">{item.assignedCount} assigned</p>
                        ) : null}
                        {item.stale ? (
                          <p className="text-200 text-text-primary-50">Configuration changed</p>
                        ) : item.shares.partial && (!state.historyPending || item.shares.accepted !== undefined) ? (
                          <p className="text-200 text-text-primary-50">
                            {item.shares.accepted === undefined ? "History unavailable" : "Partial history"}
                          </p>
                        ) : null}
                      </div>
                    ),
                  },
                  active: {
                    width: "w-1/3 min-w-56 phone:min-w-24",
                    allowWrap: true,
                    component: (item) => (
                      <div className="break-all">
                        {activePool(item)}
                        {item.observedBoards !== undefined && item.observedBoards < item.assignedCount ? (
                          <p className="mt-1 text-200 text-text-primary-50">
                            {item.observedBoards} of {item.assignedCount} hashboards reporting
                          </p>
                        ) : null}
                      </div>
                    ),
                  },
                  assigned: { width: "w-32", component: (item) => <span>{item.assignedCount}</span> },
                  rate: {
                    width: "w-28 phone:min-w-20",
                    component: (item) => (
                      <span>
                        {item.shares.acceptanceRate === undefined
                          ? "—"
                          : `${(item.shares.acceptanceRate * 100).toFixed(1)}%`}
                      </span>
                    ),
                  },
                  accepted: { width: "w-24", component: (item) => <span>{number(item.shares.accepted)}</span> },
                  rejected: { width: "w-24", component: (item) => <span>{number(item.shares.rejected)}</span> },
                }}
              />
              <PageControls
                previous={state.page > 0}
                next={(state.page + 1) * 25 < state.configurations.length}
                onPrevious={() => setPage(state.page - 1)}
                onNext={() => setPage(state.page + 1)}
              />
            </>
          ) : null}
        </div>
      )}
    </div>
  );
}
