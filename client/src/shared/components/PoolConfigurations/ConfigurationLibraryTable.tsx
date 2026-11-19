import type { ReactNode } from "react";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

export interface LibraryConfiguration {
  configurationId: string;
  name: string;
  pools: ReadonlyArray<{ url: string }>;
  isDefault?: boolean;
  assignedCount: bigint | number;
}

export function ConfigurationLibraryTable<T extends LibraryConfiguration>({
  configurations,
  onView,
  renderActions,
  emptyState,
}: {
  configurations: T[];
  onView: (configuration: T) => void;
  renderActions?: (configuration: T) => ReactNode;
  emptyState: ReactNode;
}) {
  const { isPhone } = useWindowDimensions();
  const columns: Array<"name" | "destination" | "assigned" | "actions"> = ["name", "destination"];
  if (!isPhone) columns.push("assigned");
  if (renderActions) columns.push("actions");
  return (
    <List<T, string, "name" | "destination" | "assigned" | "actions">
      items={configurations}
      itemKey="configurationId"
      itemSelectable={false}
      stickyFirstColumn={false}
      applyColumnWidthsToCells
      tableClassName="mb-0"
      activeCols={columns}
      colTitles={{ name: "Name", destination: "Primary pool", assigned: "Assigned equipment", actions: "" }}
      colConfig={{
        name: {
          width: "w-1/3 min-w-40 phone:min-w-24",
          allowWrap: true,
          component: (configuration) => (
            <div className="flex flex-col items-start">
              <Button
                variant="textOnly"
                textColor="text-text-primary"
                className="justify-start text-left font-normal !whitespace-normal"
                textOnlyUnderlineOnHover={false}
                text={configuration.name}
                onClick={() => onView(configuration)}
              />
              {isPhone ? (
                <span className="text-200 text-text-primary-50">{configuration.assignedCount.toString()} assigned</span>
              ) : null}
              {configuration.isDefault ? <span className="text-200 text-text-primary-50">Default</span> : null}
              {configuration.pools.length > 1 ? (
                <span className="text-200 text-text-primary-50">
                  {configuration.pools.length - 1} backup{configuration.pools.length > 2 ? "s" : ""}
                </span>
              ) : null}
            </div>
          ),
        },
        destination: {
          width: "w-1/3 min-w-56 phone:min-w-24",
          allowWrap: true,
          component: (configuration) => <span className="break-all">{configuration.pools[0]?.url || "—"}</span>,
        },
        assigned: {
          width: "w-1/3 min-w-40 phone:min-w-20",
          component: (configuration) => <span>{configuration.assignedCount.toString()}</span>,
        },
        actions: { width: "w-12", component: (configuration) => renderActions?.(configuration) },
      }}
      noDataElement={emptyState}
    />
  );
}
