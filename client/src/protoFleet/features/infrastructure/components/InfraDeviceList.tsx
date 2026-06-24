import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import AddInfraDeviceModal from "./AddInfraDevice/AddInfraDeviceModal";
import InfraDeviceDetailModal from "./InfraDeviceDetail/InfraDeviceDetailModal";
import ManageColumnsModal, { type InfraColumnPreference } from "./ManageColumnsModal";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import type { InfraDeviceDraft, InfraDeviceItem } from "@/protoFleet/features/infrastructure/types";
import { Alert, ChevronDown, Plus, Slider } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ActiveFilters, FilterItem, NestedFilterDropdownItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { SORT_ASC, type SortDirection } from "@/shared/components/List/types";
import StatusCircle from "@/shared/components/StatusCircle";
import Switch from "@/shared/components/Switch";

const infraCols = {
  name: "name",
  id: "id",
  endpoint: "endpoint",
  port: "port",
  site: "site",
  building: "building",
  type: "type",
  enabled: "enabled",
  status: "status",
  lastSeen: "lastSeen",
} as const;

type InfraColumn = (typeof infraCols)[keyof typeof infraCols];

const infraColTitles: ColTitles<InfraColumn> = {
  name: "Name",
  id: "ID",
  endpoint: "Endpoint",
  port: "Port",
  site: "Site",
  building: "Building",
  type: "Type",
  enabled: "Enabled",
  status: "Status",
  lastSeen: "Last seen",
};

const DEFAULT_VISIBLE: InfraColumn[] = [
  "name",
  "status",
  "lastSeen",
  "site",
  "building",
  "type",
  "enabled",
  "endpoint",
  "port",
  "id",
];
const CONFIGURABLE_COLS: InfraColumn[] = [
  "status",
  "lastSeen",
  "site",
  "building",
  "type",
  "enabled",
  "endpoint",
  "port",
  "id",
];

const STATUS_OPTIONS = [
  { id: "online", label: "Online" },
  { id: "offline", label: "Offline" },
];

const ENABLED_OPTIONS = [
  { id: "auto", label: "Auto/on" },
  { id: "off", label: "Off" },
];

const TYPE_OPTIONS = [
  { id: "single_fan", label: "Single fan" },
  { id: "fan_group", label: "Fan group" },
];

const statusToCircle = (status: string) => {
  switch (status) {
    case "online":
      return "normal" as const;
    case "offline":
      return "error" as const;
    default:
      return "inactive" as const;
  }
};

const formatStatus = (status: string) => (status === "online" ? "Online" : "Offline");

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

const getDefaultSortDirection = (_column: InfraColumn): SortDirection => SORT_ASC;

const firstColumnPadding = { phone: "16px", tablet: "16px", laptop: "16px", desktop: "16px" };
const fleetChromePadding = { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" };
const infraItemName = { singular: "device", plural: "devices" };
const columnsExemptFromDisabledStyling = new Set<InfraColumn>([infraCols.name, infraCols.status, infraCols.enabled]);

const PAGE_SIZE = 50;
const EMPTY_DEVICES: InfraDeviceItem[] = [];
const EMPTY_ACTIVE_FILTERS: ActiveFilters = {
  buttonFilters: [],
  dropdownFilters: {},
  numericFilters: {},
  textareaListFilters: {},
};

interface InfraDeviceListProps {
  devices?: InfraDeviceItem[];
  canManage?: boolean;
}

const buildDefaultColumnPrefs = () =>
  CONFIGURABLE_COLS.map((c) => ({ id: c, label: infraColTitles[c], visible: DEFAULT_VISIBLE.includes(c) }));

const hasAnyActiveFilters = (filters: ActiveFilters) =>
  filters.buttonFilters.length > 0 ||
  Object.values(filters.dropdownFilters).some((values) => values.length > 0) ||
  Object.keys(filters.numericFilters).length > 0 ||
  Object.values(filters.textareaListFilters).some((values) => values.length > 0);

const uniqueSorted = (values: string[]) => [...new Set(values.filter(Boolean))].sort();

const slugify = (value: string) =>
  value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");

const buildDeviceId = (draft: InfraDeviceDraft, devices: InfraDeviceItem[]) => {
  const existingIds = new Set(devices.map((device) => device.id));
  const baseId = slugify(`${draft.siteName}-${draft.buildingName}-${draft.name}`) || "infrastructure-device";
  let id = baseId;
  let suffix = 2;

  while (existingIds.has(id)) {
    id = `${baseId}-${suffix}`;
    suffix += 1;
  }

  return id;
};

const InfraDeviceList = ({ devices = EMPTY_DEVICES, canManage = true }: InfraDeviceListProps) => {
  const [devicesPropSnapshot, setDevicesPropSnapshot] = useState(devices);
  const [localDevices, setLocalDevices] = useState<InfraDeviceItem[]>(() => devices);
  const [detailDeviceId, setDetailDeviceId] = useState<string | null>(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showManageColumns, setShowManageColumns] = useState(false);
  const [currentPage, setCurrentPage] = useState(0);
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>(EMPTY_ACTIVE_FILTERS);
  const [currentSort, setCurrentSort] = useState<{ field: InfraColumn; direction: SortDirection }>({
    field: "name",
    direction: SORT_ASC,
  });
  const defaultColumnPrefs = useMemo(() => buildDefaultColumnPrefs(), []);
  const [columnPrefs, setColumnPrefs] = useState<InfraColumnPreference[]>(() => buildDefaultColumnPrefs());

  if (devices !== devicesPropSnapshot) {
    const deviceIds = new Set(devices.map((device) => device.id));
    setDevicesPropSnapshot(devices);
    setLocalDevices(devices);
    if (detailDeviceId && !deviceIds.has(detailDeviceId)) {
      setDetailDeviceId(null);
    }
    setCurrentPage(0);
  }

  const detailDevice = useMemo(
    () => localDevices.find((device) => device.id === detailDeviceId) ?? null,
    [localDevices, detailDeviceId],
  );
  const siteOptions = useMemo(() => uniqueSorted(localDevices.map((device) => device.siteName)), [localDevices]);
  const buildingOptions = useMemo(
    () => uniqueSorted(localDevices.map((device) => device.buildingName)),
    [localDevices],
  );

  const updateDevice = useCallback((updated: InfraDeviceItem) => {
    setLocalDevices((prev) => prev.map((device) => (device.id === updated.id ? updated : device)));
  }, []);

  const deleteDevices = useCallback((deviceIds: string[]) => {
    const ids = new Set(deviceIds);
    setLocalDevices((prev) => prev.filter((device) => !ids.has(device.id)));
    setDetailDeviceId((id) => (id && ids.has(id) ? null : id));
    setCurrentPage(0);
  }, []);

  const setEnabledMode = useCallback((deviceId: string, enabled: boolean) => {
    setLocalDevices((prev) =>
      prev.map((device) => (device.id === deviceId ? { ...device, enabled: enabled ? "auto" : "off" } : device)),
    );
  }, []);

  const addDevice = useCallback((draft: InfraDeviceDraft) => {
    setLocalDevices((prev) => [
      {
        ...draft,
        id: buildDeviceId(draft, prev),
        status: "offline",
        enabled: "auto",
        lastSeen: "Never",
      },
      ...prev,
    ]);
    setShowAddModal(false);
    setCurrentPage(0);
  }, []);

  const getRowActions = useCallback(
    (device: InfraDeviceItem): RowAction[] => [
      { label: canManage ? "Edit" : "View details", onClick: () => setDetailDeviceId(device.id) },
      ...(canManage ? [{ label: "Delete", onClick: () => deleteDevices([device.id]) }] : []),
    ],
    [canManage, deleteDevices],
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
              {device.status === "offline" ? (
                <Alert width="w-4" className="shrink-0 text-intent-critical-fill" />
              ) : null}
              <RowActionsMenu actions={getRowActions(device)} ariaLabel={`Actions for ${device.name}`} />
            </div>
          </div>
        ),
        width: "w-[260px]",
      },
      [infraCols.id]: {
        component: (device) => <span className="text-300 text-text-primary">{device.id}</span>,
        width: "w-[180px]",
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
        component: (device) => <span className="text-300 text-text-primary">{device.endpoint}</span>,
        width: "w-[120px]",
      },
      [infraCols.port]: {
        component: (device) => <span className="text-300 text-text-primary">{device.port}</span>,
        width: "w-[120px]",
      },
      [infraCols.lastSeen]: {
        component: (device) => <span className="text-300 text-text-primary">{device.lastSeen}</span>,
        width: "w-[120px]",
      },
      [infraCols.status]: {
        component: (device) => (
          <div className="flex items-center gap-2">
            <StatusCircle status={statusToCircle(device.status)} variant="simple" width="w-[6px]" />
            <span>{formatStatus(device.status)}</span>
          </div>
        ),
        width: "w-[132px]",
      },
      [infraCols.enabled]: {
        component: (device) => {
          return (
            <div data-no-row-click>
              <Switch
                ariaLabel={`Enabled for ${device.name}`}
                checked={device.enabled === "auto"}
                disabled={!canManage}
                setChecked={(next) => {
                  const checked = typeof next === "function" ? next(device.enabled === "auto") : next;
                  setEnabledMode(device.id, checked);
                }}
              />
            </div>
          );
        },
        width: "w-[88px]",
      },
    }),
    [canManage, getRowActions, setEnabledMode],
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
            options: [...new Set(localDevices.map((d) => d.siteName))].sort().map((s) => ({ id: s, label: s })),
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Building",
            value: "building",
            options: [...new Set(localDevices.map((d) => d.buildingName))].sort().map((b) => ({ id: b, label: b })),
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
        ],
      } satisfies NestedFilterDropdownItem,
    ],
    [localDevices],
  );

  const filterDevice = useCallback((_device: InfraDeviceItem, _filters: ActiveFilters) => {
    const statusF = _filters.dropdownFilters["status"];
    if (statusF?.length && !statusF.includes(_device.status)) return false;
    const enabledF = _filters.dropdownFilters["enabled"];
    if (enabledF?.length && !enabledF.includes(_device.enabled)) return false;
    const typeF = _filters.dropdownFilters["type"];
    const deviceType = getDeviceType(_device);
    if (typeF?.length && (!deviceType || !typeF.includes(deviceType))) return false;
    const buildingF = _filters.dropdownFilters["building"];
    if (buildingF?.length && !buildingF.includes(_device.buildingName)) return false;
    const siteF = _filters.dropdownFilters["site"];
    if (siteF?.length && !siteF.includes(_device.siteName)) return false;
    return true;
  }, []);

  const sortedDevices = useMemo(() => {
    const fieldToKey: Partial<Record<InfraColumn, keyof InfraDeviceItem>> = {
      name: "name",
      id: "id",
      site: "siteName",
      building: "buildingName",
      endpoint: "endpoint",
      port: "port",
      lastSeen: "lastSeen",
      status: "status",
      enabled: "enabled",
    };
    return [...localDevices].sort((a, b) => {
      const key = fieldToKey[currentSort.field];
      const aVal =
        currentSort.field === "enabled"
          ? a.enabled
          : currentSort.field === "type"
            ? formatDeviceType(a)
            : key
              ? a[key]
              : "";
      const bVal =
        currentSort.field === "enabled"
          ? b.enabled
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
  }, [localDevices, currentSort]);

  const handleSort = useCallback((field: InfraColumn, direction: SortDirection) => {
    setCurrentSort({ field, direction });
    setCurrentPage(0);
  }, []);

  const filteredDevices = useMemo(
    () => sortedDevices.filter((device) => filterDevice(device, activeFilters)),
    [activeFilters, filterDevice, sortedDevices],
  );
  const filtersAreActive = useMemo(() => hasAnyActiveFilters(activeFilters), [activeFilters]);

  const totalDevices = filteredDevices.length;
  const maxPage = Math.max(Math.ceil(totalDevices / PAGE_SIZE) - 1, 0);
  const currentPageIndex = Math.min(currentPage, maxPage);
  const paginatedDevices = useMemo(
    () => filteredDevices.slice(currentPageIndex * PAGE_SIZE, (currentPageIndex + 1) * PAGE_SIZE),
    [currentPageIndex, filteredDevices],
  );

  const handleFilterChange = useCallback(async (filters: ActiveFilters) => {
    setActiveFilters(filters);
    setCurrentPage(0);
  }, []);

  const handleRowClick = useCallback((device: InfraDeviceItem) => {
    setDetailDeviceId(device.id);
  }, []);

  const hasPreviousPage = currentPageIndex > 0;
  const hasNextPage = currentPageIndex < maxPage;
  const firstItemIndex = currentPageIndex * PAGE_SIZE + 1;
  const lastItemIndex = Math.min((currentPageIndex + 1) * PAGE_SIZE, totalDevices);
  const shouldRenderPagination = totalDevices > PAGE_SIZE;

  return (
    <div className="flex flex-col">
      <List
        items={paginatedDevices}
        itemKey="id"
        activeCols={allActiveCols}
        colTitles={infraColTitles}
        colConfig={colConfig}
        filters={filters}
        onServerFilter={handleFilterChange}
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
            {canManage ? (
              <Button
                text="Add device"
                variant={variants.secondary}
                size={buttonSizes.compact}
                onClick={() => setShowAddModal(true)}
              />
            ) : null}
          </div>
        }
        stickyFirstColumn
        tableClassName="mb-4 inline-table w-max !min-w-fit !table-fixed"
        paddingLeft={firstColumnPadding}
        overflowContainer={false}
        stickyChromePaddingLeft={fleetChromePadding}
        stickyChromeClassName={PAGE_SCROLL_CHROME_WIDTH}
        applyColumnWidthsToCells
        total={totalDevices}
        totalDisabled={0}
        hideTotal
        itemName={infraItemName}
        hasActiveFilters={filtersAreActive}
        columnsExemptFromDisabledStyling={columnsExemptFromDisabledStyling}
        sortableColumns={SORTABLE_COLS}
        currentSort={currentSort}
        onSort={handleSort}
        getDefaultSortDirection={getDefaultSortDirection}
        onRowClick={handleRowClick}
      />

      {shouldRenderPagination ? (
        <div
          className={clsx("sticky left-0 flex flex-col items-center gap-4 pt-6 pb-6", PAGE_SCROLL_CHROME_WIDTH)}
          data-testid="infra-devices-pagination"
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
              onClick={() => setCurrentPage((p) => Math.max(p - 1, 0))}
              disabled={!hasPreviousPage}
            />
            <Button
              variant={variants.secondary}
              size={buttonSizes.compact}
              ariaLabel="Next page"
              prefixIcon={<ChevronDown className="rotate-270" />}
              onClick={() => setCurrentPage((p) => Math.min(p + 1, maxPage))}
              disabled={!hasNextPage}
            />
          </div>
        </div>
      ) : (
        <div className={clsx("sticky left-0 flex flex-col items-center pt-6 pb-6", PAGE_SCROLL_CHROME_WIDTH)}>
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
          canManage={canManage}
          onSave={updateDevice}
          onDelete={(deviceId) => deleteDevices([deviceId])}
          onDismiss={() => setDetailDeviceId(null)}
        />
      ) : null}

      {showAddModal ? <AddInfraDeviceModal onDismiss={() => setShowAddModal(false)} onSuccess={addDevice} /> : null}

      {showManageColumns ? (
        <ManageColumnsModal
          columns={columnPrefs}
          defaultColumns={defaultColumnPrefs}
          onDismiss={() => setShowManageColumns(false)}
          onSave={(updated) => {
            setColumnPrefs(updated);
            const visibleColumns = new Set<InfraColumn>([
              "name",
              ...updated.filter((column) => column.visible).map((column) => column.id as InfraColumn),
            ]);
            if (!visibleColumns.has(currentSort.field)) {
              setCurrentSort({ field: "name", direction: SORT_ASC });
            }
            setShowManageColumns(false);
          }}
        />
      ) : null}
    </div>
  );
};

export default InfraDeviceList;
