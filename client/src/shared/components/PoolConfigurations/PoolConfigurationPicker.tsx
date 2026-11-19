import { type ReactNode } from "react";

import { Plus } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Radio from "@/shared/components/Radio";
import { InfoTooltip } from "@/shared/components/Tooltip/InfoTooltip";

interface PoolConfigurationOption {
  configurationId: string;
  name: string;
  pools: ReadonlyArray<{ url: string }>;
}

const BackupPools = ({ configuration }: { configuration: PoolConfigurationOption }) => {
  const backups = configuration.pools.slice(1);
  if (!backups.length) return <span>—</span>;
  if (backups.length === 1) return <span className="break-all">{backups[0].url}</span>;
  return (
    <div className="flex items-center gap-1.5">
      <span>{backups.length} pools</span>
      <InfoTooltip
        label={`Backup pools for ${configuration.name}`}
        body={
          <ol className="list-inside list-decimal break-all">
            {backups.map((pool, index) => (
              <li key={`${index}:${pool.url}`}>{pool.url}</li>
            ))}
          </ol>
        }
      />
    </div>
  );
};

const ConfigurationOptions = <T extends PoolConfigurationOption>({
  configurations,
  selected,
  onSelect,
}: {
  configurations: T[];
  selected?: T;
  onSelect: (configuration: T) => void;
}) => (
  <List<T, string, "name" | "primary" | "backups">
    items={configurations}
    itemKey="configurationId"
    itemSelectable={false}
    stickyFirstColumn={false}
    applyColumnWidthsToCells
    tableClassName="mb-0"
    activeCols={["name", "primary", "backups"]}
    colTitles={{ name: "Name", primary: "Primary pool", backups: "Backup pool" }}
    onRowClick={onSelect}
    colConfig={{
      name: {
        width: "w-1/3 min-w-40 phone:min-w-24",
        allowWrap: true,
        component: (configuration) => (
          <label className="flex cursor-pointer items-center gap-4 phone:gap-2">
            <Radio
              name="pool-configuration"
              value={configuration.configurationId}
              selected={selected?.configurationId === configuration.configurationId}
              onChange={() => onSelect(configuration)}
            />
            <span className="text-emphasis-300 break-words">{configuration.name}</span>
          </label>
        ),
      },
      primary: {
        width: "w-1/3 min-w-40 phone:min-w-24",
        allowWrap: true,
        component: (configuration) => <span className="break-all">{configuration.pools[0]?.url || "—"}</span>,
      },
      backups: {
        width: "w-1/3 min-w-40 phone:min-w-24",
        allowWrap: true,
        component: (configuration) => <BackupPools configuration={configuration} />,
      },
    }}
    noDataElement={<p className="py-6 text-text-primary-70">No pool configurations yet.</p>}
  />
);

export const PoolConfigurationPicker = <T extends PoolConfigurationOption>({
  selectionDescription,
  mixed,
  onCreate,
  createDisabled,
  additionalAction,
  loading,
  error,
  onRetry,
  configurations,
  selected,
  onSelect,
}: {
  selectionDescription: string;
  mixed?: boolean;
  onCreate?: () => void;
  createDisabled?: boolean;
  additionalAction?: ReactNode;
  loading: boolean;
  error?: string | null;
  onRetry: () => void;
  configurations?: T[];
  selected?: T;
  onSelect: (configuration: T) => void;
}) => (
  <>
    <p>{selectionDescription}</p>
    {mixed ? <p>Mixed configurations</p> : null}
    {onCreate || additionalAction ? (
      <div className="flex flex-wrap gap-3">
        {onCreate ? (
          <Button
            variant="secondary"
            prefixIcon={<Plus />}
            text="New pool configuration"
            disabled={createDisabled}
            onClick={onCreate}
          />
        ) : null}
        {additionalAction}
      </div>
    ) : null}
    {loading ? (
      <div className="flex justify-center py-10">
        <ProgressCircular indeterminate />
      </div>
    ) : null}
    {error ? (
      <div role="alert">
        <p>{error}</p>
        <Button variant="secondary" text="Retry" onClick={onRetry} />
      </div>
    ) : null}
    {configurations ? (
      <ConfigurationOptions configurations={configurations} selected={selected} onSelect={onSelect} />
    ) : null}
  </>
);
