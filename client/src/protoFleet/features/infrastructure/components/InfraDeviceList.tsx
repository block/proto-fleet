import { useCallback, useMemo, useRef, useState } from "react";
import clsx from "clsx";

import InfraDeviceDetailModal from "./InfraDeviceDetail/InfraDeviceDetailModal";
import AddInfraDeviceModal from "./AddInfraDevice/AddInfraDeviceModal";
import ManageColumnsModal, { type InfraColumnPreference } from "./ManageColumnsModal";
import { mockInfraDevices } from "@/protoFleet/features/maintenance/mockData";
import ActionBar from "@/protoFleet/features/fleetManagement/components/ActionBar";
import { Alert, ChevronDown, Plus, Slider } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import List, { type SelectionMode } from "@/shared/components/List";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { SORT_ASC, SORT_DESC, type SortDirection } from "@/shared/components/List/types";
import type { ActiveFilters, FilterItem, NestedFilterDropdownItem } from "@/shared/components/List/Filters/types";
import StatusCircle from "@/shared/components/StatusCircle";
import { useSetActionBarVisible } from "@/protoFleet/store";

const infraCols = {
  name: "name",
  ipAddress: "ipAddress",
  type: "type",
  model: "model",
  building: "building",
  site: "site",
  status: "status",
  issues: "issues",
  reading: "reading",
  powerUsage: "powerUsage",
  temperature: "temperature",
  firmware: "firmware",
  lastSeen: "lastSeen",
} as const;

type InfraColumn = (typeof infraCols)[keyof typeof infraCols];

interface InfraDeviceItem {
  id: string;
  name: string;
  deviceType: string;
  subtype: string;
  model: string;
  buildingName: string;
  siteName: string;
  ipAddress: string;
  status: string;
  issues: number;
  rpm: number | null;
  powerW: number | null;
  temperatureC: number | null;
  firmware: string;
  lastSeen: string;
}

const infraColTitles: ColTitles<InfraColumn> = {
  name: "Name",
  ipAddress: "IP address",
  type: "Type",
  model: "Model",
  building: "Building",
  site: "Site",
  status: "Status",
  issues: "Issues",
  reading: "Reading",
  powerUsage: "Power",
  temperature: "Temp",
  firmware: "Firmware",
  lastSeen: "Last seen",
};

const DEFAULT_VISIBLE: InfraColumn[] = ["name", "ipAddress", "type", "building", "site", "status", "issues", "reading"];
const CONFIGURABLE_COLS: InfraColumn[] = ["ipAddress", "type", "model", "building", "site", "status", "issues", "reading", "powerUsage", "temperature", "firmware", "lastSeen"];

const STATUS_OPTIONS = [
  { id: "online", label: "Online" },
  { id: "degraded", label: "Degraded" },
  { id: "offline", label: "Offline" },
];

const TYPE_OPTIONS = [
  { id: "fan", label: "Fan" },
  { id: "sensor", label: "Sensor" },
  { id: "pdu", label: "PDU" },
];

const statusToCircle = (status: string) => {
  switch (status) {
    case "online":
      return "normal" as const;
    case "degraded":
      return "warning" as const;
    default:
      return "inactive" as const;
  }
};

const SORTABLE_COLS = new Set<InfraColumn>(Object.values(infraCols));

const DEFAULT_SORT_DIRECTIONS: Partial<Record<InfraColumn, SortDirection>> = {
  issues: SORT_DESC,
  reading: SORT_DESC,
  powerUsage: SORT_DESC,
  temperature: SORT_DESC,
};

const getDefaultSortDirection = (column: InfraColumn): SortDirection =>
  DEFAULT_SORT_DIRECTIONS[column] ?? SORT_ASC;

const paddingLeft = { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" };

const PAGE_SIZE = 50;

const InfraDeviceList = () => {
  const [devices] = useState<InfraDeviceItem[]>(mockInfraDevices);
  const [detailDeviceId, setDetailDeviceId] = useState<string | null>(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [showManageColumns, setShowManageColumns] = useState(false);
  const [selectionMode, setSelectionMode] = useState<SelectionMode>("none");
  const [currentSort, setCurrentSort] = useState<{ field: InfraColumn; direction: SortDirection }>({ field: "name", direction: SORT_ASC });
  const [columnPrefs, setColumnPrefs] = useState<InfraColumnPreference[]>(
    () => CONFIGURABLE_COLS.map((c) => ({ id: c, label: infraColTitles[c], visible: DEFAULT_VISIBLE.includes(c) })),
  );

  const setActionBarVisible = useSetActionBarVisible();
  const selectedCountRef = useRef(0);

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
              className={clsx("min-w-0 cursor-pointer truncate text-left hover:underline", { "opacity-50": device.status === "offline" })}
              title={device.name}
              onClick={() => setDetailDeviceId(device.id)}
            >
              {device.name}
            </button>
            <div className="flex items-center gap-2">
              {device.issues > 0 && device.status !== "offline" ? (
                <Alert width="w-4" className="text-text-critical" />
              ) : null}
              <RowActionsMenu actions={getRowActions(device)} ariaLabel={`Actions for ${device.name}`} />
            </div>
          </div>
        ),
        width: "w-[208px]",
      },
      [infraCols.ipAddress]: {
        component: (device) => <span className="text-300 text-text-primary-70">{device.ipAddress}</span>,
        width: "w-[148px]",
      },
      [infraCols.type]: {
        component: (device) => <span className="text-300 capitalize">{device.subtype || device.deviceType}</span>,
        width: "w-[176px]",
      },
      [infraCols.model]: {
        component: (device) => <span className="text-300">{device.model || "—"}</span>,
        width: "w-[176px]",
      },
      [infraCols.building]: {
        component: (device) => <span className="text-300">{device.buildingName}</span>,
        width: "w-[148px]",
      },
      [infraCols.site]: {
        component: (device) => <span className="text-300">{device.siteName}</span>,
        width: "w-[120px]",
      },
      [infraCols.status]: {
        component: (device) => (
          <div className="flex items-center gap-2">
            <StatusCircle status={statusToCircle(device.status)} variant="simple" width="w-[6px]" />
            <span className="capitalize">{device.status}</span>
          </div>
        ),
        width: "w-[120px]",
      },
      [infraCols.issues]: {
        component: (device) => {
          if (device.status === "degraded") {
            return (
              <button
                type="button"
                className="flex cursor-pointer items-center gap-2 hover:underline"
                onClick={() => setDetailDeviceId(device.id)}
              >
                <Alert width="w-4" />
                Degraded performance
              </button>
            );
          }
          if (device.status === "offline") {
            return (
              <button
                type="button"
                className="flex cursor-pointer items-center gap-2 hover:underline"
                onClick={() => setDetailDeviceId(device.id)}
              >
                Connection lost
              </button>
            );
          }
          if (device.issues > 0) {
            return (
              <button
                type="button"
                className="flex cursor-pointer items-center gap-2 hover:underline"
                onClick={() => setDetailDeviceId(device.id)}
              >
                <Alert width="w-4" />
                {device.issues} issue{device.issues !== 1 ? "s" : ""}
              </button>
            );
          }
          return null;
        },
        width: "w-[200px]",
      },
      [infraCols.reading]: {
        component: (device) => (
          <span className="text-300">{device.rpm != null ? `${device.rpm.toLocaleString()} RPM` : "—"}</span>
        ),
        width: "w-[104px]",
      },
      [infraCols.powerUsage]: {
        component: (device) => (
          <span className="text-300">{device.powerW != null ? `${device.powerW.toLocaleString()} W` : "—"}</span>
        ),
        width: "w-[80px]",
      },
      [infraCols.temperature]: {
        component: (device) => (
          <span className="text-300">{device.temperatureC != null ? `${device.temperatureC}°C` : "—"}</span>
        ),
        width: "w-[80px]",
      },
      [infraCols.firmware]: {
        component: (device) => <span className="text-300">{device.firmware || "—"}</span>,
        width: "w-[120px]",
      },
      [infraCols.lastSeen]: {
        component: (device) => <span className="text-300 text-text-primary-70">{device.lastSeen || "—"}</span>,
        width: "w-[136px]",
      },
    }),
    [getRowActions],
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
            title: "Status",
            pluralTitle: "Statuses",
            value: "status",
            options: STATUS_OPTIONS,
            defaultOptionIds: [],
          },
          {
            type: "dropdown",
            title: "Type",
            value: "type",
            options: TYPE_OPTIONS,
            defaultOptionIds: [],
            showGroupDivider: true,
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
            title: "Site",
            value: "site",
            options: [...new Set(devices.map((d) => d.siteName))].sort().map((s) => ({ id: s, label: s })),
            defaultOptionIds: [],
          },
        ],
      } satisfies NestedFilterDropdownItem,
    ],
    [devices],
  );

  const filterDevice = useCallback((_device: InfraDeviceItem, _filters: ActiveFilters) => {
    const statusF = _filters.dropdownFilters["status"];
    if (statusF?.length && !statusF.includes(_device.status)) return false;
    const typeF = _filters.dropdownFilters["type"];
    if (typeF?.length && !typeF.includes(_device.deviceType)) return false;
    const buildingF = _filters.dropdownFilters["building"];
    if (buildingF?.length && !buildingF.includes(_device.buildingName)) return false;
    const siteF = _filters.dropdownFilters["site"];
    if (siteF?.length && !siteF.includes(_device.siteName)) return false;
    return true;
  }, []);

  const sortedDevices = useMemo(() => {
    const fieldToKey: Record<string, keyof InfraDeviceItem> = {
      name: "name",
      ipAddress: "ipAddress",
      type: "subtype",
      model: "model",
      building: "buildingName",
      site: "siteName",
      status: "status",
      issues: "issues",
      reading: "rpm",
      powerUsage: "powerW",
      temperature: "temperatureC",
      firmware: "firmware",
      lastSeen: "lastSeen",
    };
    const key = fieldToKey[currentSort.field] ?? currentSort.field;
    return [...devices].sort((a, b) => {
      const aVal = a[key];
      const bVal = b[key];
      if (aVal == null && bVal == null) return 0;
      if (aVal == null) return 1;
      if (bVal == null) return -1;
      const cmp = typeof aVal === "number" && typeof bVal === "number"
        ? aVal - bVal
        : String(aVal).localeCompare(String(bVal), undefined, { numeric: true });
      return currentSort.direction === SORT_ASC ? cmp : -cmp;
    });
  }, [devices, currentSort]);

  const handleSort = useCallback((field: InfraColumn, direction: SortDirection) => {
    setCurrentSort({ field, direction });
  }, []);

  const handleRowClick = useCallback((device: InfraDeviceItem) => {
    setDetailDeviceId(device.id);
  }, []);

  const renderActionBar = useCallback(
    (selected: string[], clearSelection: () => void, currentSelectionMode: SelectionMode, totalSelectable?: number) => {
      selectedCountRef.current = selected.length;
      setActionBarVisible(selected.length > 0);

      return (
        <div className="flex w-full justify-center">
          <ActionBar
            className="fixed right-0 bottom-4 left-0 z-20 laptop:left-16 desktop:left-50"
            selectedItems={selected}
            selectionMode={currentSelectionMode}
            totalCount={totalSelectable}
            onClose={() => {
              clearSelection();
              setActionBarVisible(false);
            }}
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
                  onClick={() => {
                    clearSelection();
                    setActionBarVisible(false);
                  }}
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
    },
    [setActionBarVisible],
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
        paddingRight={paddingLeft}
        applyColumnWidthsToCells
        total={totalDevices}
        totalDisabled={0}
        hideTotal
        itemName={{ singular: "device", plural: "devices" }}
        columnsExemptFromDisabledStyling={new Set([infraCols.name, infraCols.status, infraCols.issues])}
        sortableColumns={SORTABLE_COLS}
        currentSort={currentSort}
        onSort={handleSort}
        getDefaultSortDirection={getDefaultSortDirection}
        onRowClick={handleRowClick}
        onSelectionModeChange={setSelectionMode}
        isRowDisabled={(device) => device.status === "offline"}
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

      {detailDeviceId !== null && (
        <InfraDeviceDetailModal deviceId={detailDeviceId} onDismiss={() => setDetailDeviceId(null)} />
      )}

      {showAddModal && (
        <AddInfraDeviceModal onDismiss={() => setShowAddModal(false)} onSuccess={() => setShowAddModal(false)} />
      )}

      {showManageColumns && (
        <ManageColumnsModal
          columns={columnPrefs}
          onDismiss={() => setShowManageColumns(false)}
          onSave={(updated) => {
            setColumnPrefs(updated);
            setShowManageColumns(false);
          }}
        />
      )}
    </div>
  );
};

export default InfraDeviceList;
