import { useEffect, useRef, useState } from "react";

import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Radio from "@/shared/components/Radio";
import { positions } from "@/shared/constants";

export interface PoolConfigurationOption {
  configurationId: string;
  name: string;
  pools: ReadonlyArray<{ url: string }>;
}

const BackupPools = ({ configuration }: { configuration: PoolConfigurationOption }) => {
  const backups = configuration.pools.slice(1);
  const [open, setOpen] = useState(false);
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const closeTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const show = () => {
    clearTimeout(closeTimer.current);
    setOpen(true);
  };
  const hide = () => {
    closeTimer.current = setTimeout(() => setOpen(false), 150);
  };
  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
    return () => clearTimeout(closeTimer.current);
  }, [setPopoverRenderMode]);
  if (!backups.length) return <span>—</span>;
  if (backups.length === 1) return <span className="break-all">{backups[0].url}</span>;
  return (
    <div ref={triggerRef} onMouseEnter={show} onMouseLeave={hide} onFocus={show} onBlur={hide}>
      <Button
        variant="textOnly"
        textColor="text-text-primary"
        text={`${backups.length} pools`}
        ariaLabel={`Backup pools for ${configuration.name}`}
        ariaExpanded={open}
        onClick={show}
      />
      {open ? (
        <Popover
          size={popoverSizes.small}
          position={positions["bottom left"]}
          className="!rounded-2xl"
          closePopover={() => setOpen(false)}
        >
          <div onMouseEnter={show} onMouseLeave={hide}>
            <ol
              aria-label={`Backup pools for ${configuration.name}`}
              className="list-inside list-decimal space-y-3 break-all"
            >
              {backups.map((pool, index) => (
                <li key={`${index}:${pool.url}`}>{pool.url}</li>
              ))}
            </ol>
          </div>
        </Popover>
      ) : null}
    </div>
  );
};

export const ConfigurationOptions = <T extends PoolConfigurationOption>({
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
        component: (configuration) => (
          <PopoverProvider>
            <BackupPools configuration={configuration} />
          </PopoverProvider>
        ),
      },
    }}
    noDataElement={<p className="py-6 text-text-primary-70">No pool configurations yet.</p>}
  />
);
