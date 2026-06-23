import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";

import AddInfraDeviceModal from "./AddInfraDevice/AddInfraDeviceModal";
import InfraDeviceDetailModal from "./InfraDeviceDetail/InfraDeviceDetailModal";
import ManageColumnsModal, { type InfraColumnPreference } from "./ManageColumnsModal";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import type { InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { useSetActionBarVisible } from "@/protoFleet/store";
import { Alert, ChevronDown, Plus, Slider } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List, { type SelectionMode } from "@/shared/components/List";
import type { ActiveFilters, FilterItem, NestedFilterDropdownItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";
import StatusCircle from "@/shared/components/StatusCircle";
import Switch from "@/shared/components/Switch";

const infraCols = {
  name: "name",
  endpoint: "endpoint",
  port: "port",
  site: "site",
  building: "building",
  type: "type",
  enabled: "enabled",
  status: "status",
  issues: "issues",
  lastSeen: "lastSeen",
} as const;

type InfraColumn = (typeof infraCols)[keyof typeof infraCols];

const infraColTitles: ColTitles<InfraColumn> = {
  name: "Name",
  endpoint: "Endpoint",
  port: "Port",
  site: "Site",
  building: "Building",
  type: "Type",
  enabled: "Enabled",
  status: "Status",
  issues: "Issues",
  lastSeen: "Last seen",
};

const DEFAULT_VISIBLE: InfraColumn[] = [
  "name",
  "endpoint",
  "port",
  "site",
  "building",
  "type",
  "enabled",
  "status",
  "issues",
  "lastSeen",
];
const CONFIGURABLE_COLS: InfraColumn[] = [
  "endpoint",
  "port",
  "site",
  "building",
  "type",
  "enabled",
  "status",
  "issues",
  "lastSeen",
];

const STATUS_OPTIONS = [
  { id: "running", label: "Running" },
  { id: "stopped", label: "Stopped" },
  { id: "faulted", label: "Faulted" },
  { id: "unknown", label: "Unknown" },
];

const ENABLED_OPTIONS = [
  { id: "auto", label: "Auto/on" },
  { id: "off", label: "Off" },
];

const TYPE_OPTIONS = [
  { id: "single_fan", label: "Single fan" },
  { id: "fan_group", label: "Fan group" },
];

const ISSUE_OPTIONS = [
  { id: "pending", label: "Pending" },
  { id: "acked", label: "Acked" },
  { id: "failed", label: "Failed" },
  { id: "timed_out", label: "Timed out" },
];

const statusToCircle = (status: string) => {
  switch (status) {
    case "running":
      return "normal" as const;
    case "faulted":
      return "error" as const;
    case "unknown":
      return "warning" as const;
    default:
      return "inactive" as const;
  }
};

const formatStatus = (status: string) => status.replaceAll("_", " ");

const formatIssueStatus = (status: string | null) => {
  if (!status) return "";
  return status.replaceAll("_", " ");
};

const getDeviceType = (device: InfraDeviceItem) => {
  if (device.endpointKind) return device.endpointKind;
  if (device.fanCount === 1) return "single_fan";
  if (device.fanCount && device.fanCount > 1) return "fan_group";
  return null;
};

const formatDeviceType = (device: InfraDeviceItem) => {
  const type = getDeviceType(device);
  if (type === "single_fan") return "Fan";
  if (device.fanCount && device.fanCount > 1) return `Fan group (${device.fanCount} fans)`;
  if (type === "fan_group") return "Fan group";
  return "";
};

const SORTABLE_COLS = new Set<InfraColumn>(Object.values(infraCols));

const DEFAULT_SORT_DIRECTIONS: Partial<Record<InfraColumn, SortDirection>> = {
  issues: SORT_DESC,
};

const getDefaultSortDirection = (column: InfraColumn): SortDirection => DEFAULT_SORT_DIRECTIONS[column] ?? SORT_ASC;

const paddingLeft = { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" };
const infraItemName = { singular: "device", plural: "devices" };
const columnsExemptFromDisabledStyling = new Set<InfraColumn>([
  infraCols.name,
  infraCols.status,
  infraCols.enabled,
  infraCols.issues,
]);

const PAGE_SIZE = 50;

interface InfraDeviceListProps {
  devices?: InfraDeviceItem[];
}

const uniqueSorted = (values: string[]) => [...new Set(values.filter(Boolean))].sort();

const getBuildingOptionsBySite = (devices: InfraDeviceItem[]) =>
  devices.reduce<Record<string, string[]>>((acc, device) => {
    const siteBuildings = acc[device.siteName] ?? [];
    if (!siteBuildings.includes(device.buildingName)) {
      acc[device.siteName] = [...siteBuildings, device.buildingName].sort();
    }
    return acc;
  }, {});

interface InfraDeviceActionBarProps {
  selected: string[];
  clearSelection: () => void;
  selectionMode: SelectionMode;
  totalSelectable?: number;
}

const InfraDeviceActionBar = ({
  selected,
  clearSelection,
  selectionMode,
  totalSelectable,
}: InfraDeviceActionBarProps) => {
  const setActionBarVisible = useSetActionBarVisible();

  useEffect(() => {
    setActionBarVisible(selected.length > 0);
  }, [selected.length, setActionBarVisible]);

  useEffect(() => {
    return () => setActionBarVisible(false);
  }, [setActionBarVisible]);

  return (
    <div className="flex w-full justify-center">
      <ActionBar
        className="fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50"
        selectedItems={selected}
        selectionMode={selectionMode}
        totalCount={totalSelectable}
        onClose={clearSelection}
        selectionControls={
          <>
            <Button
              className="py-1"
              size={buttonSizes.textOnly}
              variant={variants.textOnly}
              textColor="text-core-accent-fill"
              textOnlyUnderlineOnHover={false}
              onClick={() => {}}
            >
              Select all
            </Button>
            <Button
              className="py-1"
              size={buttonSizes.textOnly}
              variant={variants.textOnly}
              textColor="text-core-accent-fill"
              textOnlyUnderlineOnHover={false}
              onClick={clearSelection}
            >
              Select none
            </Button>
          </>
        }
        renderActions={() => (
          <Button
            className="bg-grayscale-white-10! text-grayscale-white-90!"
            text="Delete"
            variant={variants.danger}
            size={buttonSizes.compact}
          />
        )}
      />
    </div>
  );
};

const InfraDeviceList = ({ devices = [] }: InfraDeviceListProps) => {
  const [detailDeviceId, setDetailDeviceId] = useState<string | null>(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showManageColumns, setShowManageColumns] = useState(false);
  const [selectionMode, setSelectionMode] = useState<SelectionMode>("none");
  const [currentSort, setCurrentSort] = useState<{ field: InfraColumn; direction: SortDirection }>({
    field: "name",
    direction: SORT_ASC,
  });
  const [enabledOverrides, setEnabledOverrides] = useState<Record<string, InfraDeviceItem["enabled"]>>({});
  const [columnPrefs, setColumnPrefs] = useState<InfraColumnPreference[]>(() =>
    CONFIGURABLE_COLS.map((c) => ({ id: c, label: infraColTitles[c], visible: DEFAULT_VISIBLE.includes(c) })),
  );

  const detailDevice = useMemo(
    () => devices.find((device) => device.id === detailDeviceId) ?? null,
    [devices, detailDeviceId],
  );
  const siteOptions = useMemo(() => uniqueSorted(devices.map((device) => device.siteName)), [devices]);
  const buildingOptions = useMemo(() => uniqueSorted(devices.map((device) => device.buildingName)), [devices]);
  const buildingOptionsBySite = useMemo(() => getBuildingOptionsBySite(devices), [devices]);
  const getEnabledMode = useCallback(
    (device: InfraDeviceItem) => enabledOverrides[device.id] ?? device.enabled,
    [enabledOverrides],
  );
  const setEnabledMode = useCallback((deviceId: string, enabled: boolean) => {
    setEnabledOverrides((prev) => ({ ...prev, [deviceId]: enabled ? "auto" : "off" }));
  }, []);

  const getRowActions = useCallback(
    (device: InfraDeviceItem): RowAction[] => [
      { label: "Edit", onClick: () => setDetailDeviceId(device.id) },
      { label: "Test connection", onClick: () => {} },
      { label: "Delete", onClick: () => {} },
    ],
    [],
  );

  const allActiveCols: InfraColumn[] = useMemo(
    () => ["name" as InfraColumn, ...columnPrefs.filter((c) => c.visible).map((c) => c.id as InfraColumn)],
    [columnPrefs],
  );

  const colConfig: ColConfig<InfraDeviceItem, string, InfraColumn> = useMemo(
    () => ({
      [infraCols.name]: {
        component: (device) => (
          <div className="grid w-full grid-cols-[1fr_auto] items-center gap-3" data-no-row-click>
            <button
              type="button"
              className="min-w-0 cursor-pointer text-left hover:underline"
              title={device.name}
              onClick={() => setDetailDeviceId(device.id)}
            >
              <span className="block truncate">{device.name}</span>
            </button>
            <div className="flex items-center gap-2">
              {device.issueStatus && device.issueStatus !== "acked" ? (
                <Alert width="w-4" className="text-text-critical" />
              ) : null}
              <RowActionsMenu actions={getRowActions(device)} ariaLabel={`Actions for ${device.name}`} />
            </div>
          </div>
        ),
        width: "w-[260px]",
      },
      [infraCols.type]: {
        component: (device) => <span className="text-300">{formatDeviceType(device)}</span>,
        width: "w-[112px]",
      },
      [infraCols.site]: {
        component: (device) => <span className="text-300">{device.siteName}</span>,
        width: "w-[120px]",
      },
      [infraCols.building]: {
        component: (device) => <span className="text-300">{device.buildingName}</span>,
        width: "w-[148px]",
      },
      [infraCols.endpoint]: {
        component: (device) => <span className="font-mono text-300 text-text-primary-70">{device.endpoint}</span>,
        width: "w-[160px]",
      },
      [infraCols.port]: {
        component: (device) => <span className="font-mono text-300 text-text-primary-70">{device.port}</span>,
        width: "w-[88px]",
      },
      [infraCols.lastSeen]: {
        component: (device) => <span className="text-300 text-text-primary-70">{device.lastSeen}</span>,
        width: "w-[120px]",
      },
      [infraCols.status]: {
        component: (device) => (
          <div className="flex items-center gap-2">
            <StatusCircle status={statusToCircle(device.status)} variant="simple" width="w-[6px]" />
            <span className="capitalize">{formatStatus(device.status)}</span>
          </div>
        ),
        width: "w-[120px]",
      },
      [infraCols.enabled]: {
        component: (device) => {
          const mode = getEnabledMode(device);
          return (
            <div data-no-row-click>
              <Switch
                checked={mode === "auto"}
                setChecked={(next) => {
                  const checked = typeof next === "function" ? next(mode === "auto") : next;
                  setEnabledMode(device.id, checked);
                }}
              />
            </div>
          );
        },
        width: "w-[88px]",
      },
      [infraCols.issues]: {
        component: (device) => {
          if (!device.issueStatus) {
            return null;
          }

          const critical = device.issueStatus === "failed" || device.issueStatus === "timed_out";
          const pending = device.issueStatus === "pending";

          if (critical || pending) {
            return (
              <button
                type="button"
                className={clsx("flex cursor-pointer items-center gap-2 capitalize hover:underline", {
                  "text-text-critical": critical,
                })}
                onClick={() => setDetailDeviceId(device.id)}
              >
                <Alert width="w-4" />
                {formatIssueStatus(device.issueStatus)}
              </button>
            );
          }

          return <span className="text-300 capitalize">{formatIssueStatus(device.issueStatus)}</span>;
        },
        width: "w-[136px]",
      },
    }),
    [getEnabledMode, getRowActions, setEnabledMode],
  );

  const filters: FilterItem[] = useMemo(
    () => [
      {
        type: "nestedFilterDropdown",
        title: "Add Filter",
        value: "filters-meta",
        prefixIcon: <Plus width="w-3" />,
        children: [
          {
            type: "dropdown",
            title: "Site",
            value: "site",
            options: [...new Set(devices.map((d) => d.siteName))].sort().map((s) => ({ id: s, label: s })),
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Building",
            value: "building",
            options: [...new Set(devices.map((d) => d.buildingName))].sort().map((b) => ({ id: b, label: b })),
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Type",
            value: "type",
            options: TYPE_OPTIONS,
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Enabled",
            value: "enabled",
            options: ENABLED_OPTIONS,
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Status",
            pluralTitle: "Statuses",
            value: "status",
            options: STATUS_OPTIONS,
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Issues",
            value: "issues",
            options: ISSUE_OPTIONS,
            defaultOptionIds: [],
          },
        ],
      } satisfies NestedFilterDropdownItem,
    ],
    [devices],
  );

  const filterDevice = useCallback(
    (_device: InfraDeviceItem, _filters: ActiveFilters) => {
      const statusF = _filters.dropdownFilters["status"];
      if (statusF?.length && !statusF.includes(_device.status)) return false;
      const enabledF = _filters.dropdownFilters["enabled"];
      if (enabledF?.length && !enabledF.includes(getEnabledMode(_device))) return false;
      const typeF = _filters.dropdownFilters["type"];
      const deviceType = getDeviceType(_device);
      if (typeF?.length && (!deviceType || !typeF.includes(deviceType))) return false;
      const buildingF = _filters.dropdownFilters["building"];
      if (buildingF?.length && !buildingF.includes(_device.buildingName)) return false;
      const siteF = _filters.dropdownFilters["site"];
      if (siteF?.length && !siteF.includes(_device.siteName)) return false;
      const issuesF = _filters.dropdownFilters["issues"];
      if (issuesF?.length && (!_device.issueStatus || !issuesF.includes(_device.issueStatus))) return false;
      return true;
    },
    [getEnabledMode],
  );

  const sortedDevices = useMemo(() => {
    const fieldToKey: Partial<Record<InfraColumn, keyof InfraDeviceItem>> = {
      name: "name",
      site: "siteName",
      building: "buildingName",
      endpoint: "endpoint",
      port: "port",
      lastSeen: "lastSeen",
      status: "status",
      enabled: "enabled",
      issues: "issueStatus",
    };
    return [...devices].sort((a, b) => {
      const key = fieldToKey[currentSort.field];
      const aVal =
        currentSort.field === "enabled"
          ? getEnabledMode(a)
          : currentSort.field === "type"
            ? formatDeviceType(a)
            : key
              ? a[key]
              : "";
      const bVal =
        currentSort.field === "enabled"
          ? getEnabledMode(b)
          : currentSort.field === "type"
            ? formatDeviceType(b)
            : key
              ? b[key]
              : "";
      if (aVal == null && bVal == null) return 0;
      if (aVal == null) return 1;
      if (bVal == null) return -1;
      const cmp =
        typeof aVal === "number" && typeof bVal === "number"
          ? aVal - bVal
          : String(aVal).localeCompare(String(bVal), undefined, { numeric: true });
      return currentSort.direction === SORT_ASC ? cmp : -cmp;
    });
  }, [devices, currentSort, getEnabledMode]);

  const handleSort = useCallback((field: InfraColumn, direction: SortDirection) => {
    setCurrentSort({ field, direction });
  }, []);

  const handleRowClick = useCallback((device: InfraDeviceItem) => {
    setDetailDeviceId(device.id);
  }, []);

  const renderActionBar = useCallback(
    (selected: string[], clearSelection: () => void, currentSelectionMode: SelectionMode, totalSelectable?: number) => {
      if (selected.length === 0) {
        return null;
      }

      return (
        <InfraDeviceActionBar
          selected={selected}
          clearSelection={clearSelection}
          selectionMode={currentSelectionMode}
          totalSelectable={totalSelectable}
        />
      );
    },
    [],
  );

  // Pagination
  const totalDevices = devices.length;
  const [currentPage, setCurrentPage] = useState(0);
  const hasPreviousPage = currentPage > 0;
  const hasNextPage = (currentPage + 1) * PAGE_SIZE < totalDevices;
  const firstItemIndex = currentPage * PAGE_SIZE + 1;
  const lastItemIndex = Math.min((currentPage + 1) * PAGE_SIZE, totalDevices);
  const shouldRenderPagination = totalDevices > PAGE_SIZE;

  return (
    <div className="flex flex-col">
      <List
        items={sortedDevices}
        itemKey="id"
        activeCols={allActiveCols}
        colTitles={infraColTitles}
        colConfig={colConfig}
        filters={filters}
        filterItem={filterDevice}
        headerControls={
          <div className="flex items-center gap-2">
            <Button
              ariaLabel="Manage columns"
              ariaHasPopup="dialog"
              variant={variants.secondary}
              size={buttonSizes.compact}
              prefixIcon={<Slider width="w-4" />}
              onClick={() => setShowManageColumns(true)}
            />
            <Button
              text="Add device"
              variant={variants.secondary}
              size={buttonSizes.compact}
              onClick={() => setShowAddModal(true)}
            />
          </div>
        }
        itemSelectable
        pageScopedSelection
        stickyFirstColumn
        tableClassName="mb-4 inline-table w-max !min-w-fit !table-fixed"
        paddingLeft={paddingLeft}
        applyColumnWidthsToCells
        total={totalDevices}
        totalDisabled={0}
        hideTotal
        itemName={infraItemName}
        columnsExemptFromDisabledStyling={columnsExemptFromDisabledStyling}
        sortableColumns={SORTABLE_COLS}
        currentSort={currentSort}
        onSort={handleSort}
        getDefaultSortDirection={getDefaultSortDirection}
        onRowClick={handleRowClick}
        onSelectionModeChange={setSelectionMode}
        renderActionBar={renderActionBar}
      />

      {shouldRenderPagination ? (
        <div
          className={clsx("sticky left-0 flex flex-col items-center gap-4 pt-6", {
            "pb-24": selectionMode !== "none",
            "pb-6": selectionMode === "none",
          })}
        >
          <span className="text-300 text-text-primary">
            Showing {firstItemIndex}–{lastItemIndex} of {totalDevices} devices
          </span>
          <div className="flex gap-3">
            <Button
              variant={variants.secondary}
              size={buttonSizes.compact}
              ariaLabel="Previous page"
              prefixIcon={<ChevronDown className="rotate-90" />}
              onClick={() => setCurrentPage((p) => p - 1)}
              disabled={!hasPreviousPage}
            />
            <Button
              variant={variants.secondary}
              size={buttonSizes.compact}
              ariaLabel="Next page"
              prefixIcon={<ChevronDown className="rotate-270" />}
              onClick={() => setCurrentPage((p) => p + 1)}
              disabled={!hasNextPage}
            />
          </div>
        </div>
      ) : (
        <div className="sticky left-0 flex flex-col items-center pt-6 pb-6">
          <span className="text-300 text-text-primary">
            {totalDevices} {totalDevices === 1 ? "device" : "devices"}
          </span>
        </div>
      )}

      {detailDevice !== null ? (
        <InfraDeviceDetailModal
          device={detailDevice}
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          onDismiss={() => setDetailDeviceId(null)}
        />
      ) : null}

      {showAddModal ? (
        <AddInfraDeviceModal
          siteOptions={siteOptions}
          buildingOptions={buildingOptions}
          buildingOptionsBySite={buildingOptionsBySite}
          onDismiss={() => setShowAddModal(false)}
          onSuccess={() => setShowAddModal(false)}
        />
      ) : null}

      {showManageColumns ? (
        <ManageColumnsModal
          columns={columnPrefs}
          onDismiss={() => setShowManageColumns(false)}
          onSave={(updated) => {
            setColumnPrefs(updated);
            setShowManageColumns(false);
          }}
        />
      ) : null}
    </div>
  );
};

export default InfraDeviceList;
