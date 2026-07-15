import { useCallback, useMemo, useState } from "react";
import clsx from "clsx";

import AddInfraDeviceModal from "./AddInfraDevice/AddInfraDeviceModal";
import InfraDeviceDetailModal from "./InfraDeviceDetail/InfraDeviceDetailModal";
import ManageColumnsModal, { type InfraColumnPreference } from "./ManageColumnsModal";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { PAGE_SCROLL_CHROME_WIDTH } from "@/protoFleet/constants/layout";
import RowActionsMenu, { type RowAction } from "@/protoFleet/features/fleetManagement/components/RowActionsMenu";
import { formatDeviceType } from "@/protoFleet/features/infrastructure/deviceType";
import { summarizeDriverConfig } from "@/protoFleet/features/infrastructure/driverForms";
import {
  infraBuildingOptionsFromDevices,
  uniqueSortedLocationNames,
} from "@/protoFleet/features/infrastructure/locationOptions";
import type {
  InfraBuildingOption,
  InfraDeviceDraft,
  InfraDeviceItem,
  InfraDevicePatch,
} from "@/protoFleet/features/infrastructure/types";
import { ChevronDown, Plus, Slider } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import List from "@/shared/components/List";
import type { ActiveFilters, FilterItem, NestedFilterDropdownItem } from "@/shared/components/List/Filters/types";
import type { ColConfig, ColTitles } from "@/shared/components/List/types";
import { SORT_ASC, type SortDirection } from "@/shared/components/List/types";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Switch from "@/shared/components/Switch";
import { pushToast, STATUSES } from "@/shared/features/toaster";

const infraCols = {
  name: "name",
  site: "site",
  building: "building",
  type: "type",
  enabled: "enabled",
  connection: "connection",
} as const;

type InfraColumn = (typeof infraCols)[keyof typeof infraCols];

const infraColTitles: ColTitles<InfraColumn> = {
  name: "Name",
  site: "Site",
  building: "Building",
  type: "Target type",
  enabled: "Enabled",
  connection: "Connection",
};

// Status and Last-seen columns (and their filters and offline styling)
// are deliberately absent until v2 status read-back gives them a data
// source; v1 has none, so every device would render permanently
// offline with error styling.
const DEFAULT_VISIBLE: InfraColumn[] = ["name", "site", "building", "type", "enabled", "connection"];
const CONFIGURABLE_COLS: InfraColumn[] = ["site", "building", "type", "enabled", "connection"];

const ENABLED_OPTIONS = [
  { id: "auto", label: "Auto/on" },
  { id: "off", label: "Off" },
];

const TYPE_OPTIONS = [
  { id: "single_fan", label: "Single fan" },
  { id: "fan_group", label: "Fan group" },
];

const getConnectionSummary = (device: InfraDeviceItem) => summarizeDriverConfig(device.driverType, device.driverConfig);

const getSortValue = (device: InfraDeviceItem, field: InfraColumn) => {
  switch (field) {
    case "name":
      return device.name;
    case "site":
      return device.siteName;
    case "building":
      return device.buildingName;
    case "type":
      return formatDeviceType(device);
    case "enabled":
      return device.enabled ? 0 : 1;
    case "connection":
      return getConnectionSummary(device) ?? "";
  }
};

const SORTABLE_COLS = new Set<InfraColumn>(Object.values(infraCols));

const getDefaultSortDirection = (_column: InfraColumn): SortDirection => SORT_ASC;

const firstColumnPadding = { phone: "16px", tablet: "16px", laptop: "16px", desktop: "16px" };
const fleetChromePadding = { phone: "24px", tablet: "24px", laptop: "40px", desktop: "40px" };
const infraItemName = { singular: "device", plural: "devices" };
const columnsExemptFromDisabledStyling = new Set<InfraColumn>([infraCols.name, infraCols.enabled]);

const PAGE_SIZE = 50;
const EMPTY_DEVICES: InfraDeviceItem[] = [];
const EMPTY_UPDATING_IDS: ReadonlySet<string> = new Set();
const EMPTY_ACTIVE_FILTERS: ActiveFilters = {
  buttonFilters: [],
  dropdownFilters: {},
  numericFilters: {},
  textareaListFilters: {},
};

const noopSubmit = async () => {};

interface InfraDeviceListProps {
  devices?: InfraDeviceItem[];
  isLoading?: boolean;
  loadError?: string | null;
  onRetry?: () => void;
  canManage?: boolean;
  siteOptions?: string[];
  buildingOptions?: InfraBuildingOption[];
  initialSiteName?: string;
  updatingDeviceIds?: ReadonlySet<string>;
  onCreateDevice?: (draft: InfraDeviceDraft) => Promise<void>;
  onUpdateDevice?: (patch: InfraDevicePatch) => Promise<void>;
  onDeleteDevice?: (deviceId: string) => Promise<void>;
  onSetDeviceEnabled?: (device: InfraDeviceItem, enabled: boolean) => Promise<void>;
}

const buildDefaultColumnPrefs = () =>
  CONFIGURABLE_COLS.map((c) => ({ id: c, label: infraColTitles[c], visible: DEFAULT_VISIBLE.includes(c) }));

const hasAnyActiveFilters = (filters: ActiveFilters) =>
  filters.buttonFilters.length > 0 ||
  Object.values(filters.dropdownFilters).some((values) => values.length > 0) ||
  Object.keys(filters.numericFilters).length > 0 ||
  Object.values(filters.textareaListFilters).some((values) => values.length > 0);

const InfraDeviceList = ({
  devices = EMPTY_DEVICES,
  isLoading = false,
  loadError = null,
  onRetry,
  canManage = true,
  siteOptions,
  buildingOptions,
  initialSiteName,
  updatingDeviceIds = EMPTY_UPDATING_IDS,
  onCreateDevice = noopSubmit,
  onUpdateDevice = noopSubmit,
  onDeleteDevice = noopSubmit,
  onSetDeviceEnabled = noopSubmit,
}: InfraDeviceListProps) => {
  const [detailDeviceId, setDetailDeviceId] = useState<string | null>(null);
  // Row-menu deletes are destructive against persisted OT configuration,
  // so they confirm through a dialog naming the device before the RPC
  // fires (the detail modal's Delete has the modal itself as context).
  const [deleteCandidateId, setDeleteCandidateId] = useState<string | null>(null);
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

  const detailDevice = useMemo(
    () => devices.find((device) => device.id === detailDeviceId) ?? null,
    [devices, detailDeviceId],
  );
  const deleteCandidate = useMemo(
    () => devices.find((device) => device.id === deleteCandidateId) ?? null,
    [devices, deleteCandidateId],
  );
  const fallbackSiteOptions = useMemo(
    () => uniqueSortedLocationNames(devices.map((device) => device.siteName)),
    [devices],
  );
  const fallbackBuildingOptions = useMemo(() => infraBuildingOptionsFromDevices(devices), [devices]);
  const resolvedSiteOptions = siteOptions ?? fallbackSiteOptions;
  const resolvedBuildingOptions = buildingOptions ?? fallbackBuildingOptions;

  const handleCreateDevice = useCallback(
    async (draft: InfraDeviceDraft) => {
      await onCreateDevice(draft);
      setShowAddModal(false);
      setCurrentPage(0);
    },
    [onCreateDevice],
  );

  const handleConfirmDelete = useCallback(() => {
    if (deleteCandidateId === null) return;
    setDeleteCandidateId(null);
    // The row's actions stay disabled via updatingDeviceIds while the
    // delete is in flight; failures surface as a toast like the toggle.
    onDeleteDevice(deleteCandidateId).catch((error: unknown) => {
      pushToast({
        message: getErrorMessage(error, "Failed to delete infrastructure device."),
        status: STATUSES.error,
      });
    });
  }, [deleteCandidateId, onDeleteDevice]);

  const handleSetEnabled = useCallback(
    (device: InfraDeviceItem, enabled: boolean) => {
      onSetDeviceEnabled(device, enabled).catch((error: unknown) => {
        pushToast({
          message: getErrorMessage(error, "Failed to update infrastructure device."),
          status: STATUSES.error,
        });
      });
    },
    [onSetDeviceEnabled],
  );

  const getRowActions = useCallback(
    (device: InfraDeviceItem): RowAction[] => {
      const disabled = updatingDeviceIds.has(device.id);
      return [
        { label: canManage ? "Edit" : "View details", onClick: () => setDetailDeviceId(device.id), disabled },
        ...(canManage ? [{ label: "Delete", onClick: () => setDeleteCandidateId(device.id), disabled }] : []),
      ];
    },
    [canManage, updatingDeviceIds],
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
            <RowActionsMenu actions={getRowActions(device)} ariaLabel={`Actions for ${device.name}`} />
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
      [infraCols.connection]: {
        component: (device) => {
          const summary = getConnectionSummary(device);
          return (
            <span className="text-300 text-text-primary" title={summary ?? undefined}>
              {summary ?? "—"}
            </span>
          );
        },
        width: "w-[280px]",
      },
      [infraCols.enabled]: {
        component: (device) => {
          return (
            <div data-no-row-click>
              <Switch
                ariaLabel={`Enabled for ${device.name}`}
                checked={device.enabled}
                disabled={!canManage || updatingDeviceIds.has(device.id)}
                setChecked={(next) => {
                  const checked = typeof next === "function" ? next(device.enabled) : next;
                  handleSetEnabled(device, checked);
                }}
              />
            </div>
          );
        },
        width: "w-[88px]",
      },
    }),
    [canManage, getRowActions, handleSetEnabled, updatingDeviceIds],
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
            title: "Target type",
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
        ],
      } satisfies NestedFilterDropdownItem,
    ],
    [devices],
  );

  const filterDevice = useCallback((_device: InfraDeviceItem, _filters: ActiveFilters) => {
    const enabledF = _filters.dropdownFilters["enabled"];
    if (enabledF?.length && !enabledF.includes(_device.enabled ? "auto" : "off")) return false;
    const typeF = _filters.dropdownFilters["type"];
    if (typeF?.length && !typeF.includes(_device.deviceKind)) return false;
    const buildingF = _filters.dropdownFilters["building"];
    if (buildingF?.length && !buildingF.includes(_device.buildingName)) return false;
    const siteF = _filters.dropdownFilters["site"];
    if (siteF?.length && !siteF.includes(_device.siteName)) return false;
    return true;
  }, []);

  const sortedDevices = useMemo(() => {
    return [...devices].sort((a, b) => {
      const aVal = getSortValue(a, currentSort.field);
      const bVal = getSortValue(b, currentSort.field);
      const cmp =
        typeof aVal === "number" && typeof bVal === "number"
          ? aVal - bVal
          : String(aVal).localeCompare(String(bVal), undefined, { numeric: true });
      return currentSort.direction === SORT_ASC ? cmp : -cmp;
    });
  }, [devices, currentSort]);

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

  if (loadError) {
    return (
      <div
        className="flex min-h-[220px] flex-col items-center justify-center gap-4 py-14"
        data-testid="infra-devices-load-error"
      >
        <div className="flex flex-col items-center gap-1">
          <span className="text-emphasis-300 text-text-primary">Unable to load infrastructure devices</span>
          <span className="text-300 text-text-primary-70">{loadError}</span>
        </div>
        {onRetry ? (
          <Button text="Retry" variant={variants.secondary} size={buttonSizes.compact} onClick={onRetry} />
        ) : null}
      </div>
    );
  }

  if (isLoading) {
    return (
      <div className="flex min-h-[220px] w-full items-center justify-center py-14">
        <ProgressCircular indeterminate dataTestId="infra-devices-loading" />
      </div>
    );
  }

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

      {deleteCandidate !== null ? (
        <Dialog
          open
          title={`Delete "${deleteCandidate.name}"?`}
          subtitle={`This removes the ${formatDeviceType(deleteCandidate)} in ${deleteCandidate.buildingName} at ${deleteCandidate.siteName} from the fleet configuration. Curtailment will no longer control it.`}
          testId="infra-device-delete-dialog"
          onDismiss={() => setDeleteCandidateId(null)}
          buttons={[
            {
              text: "Cancel",
              variant: variants.secondary,
              onClick: () => setDeleteCandidateId(null),
            },
            {
              text: "Delete device",
              variant: variants.danger,
              onClick: handleConfirmDelete,
            },
          ]}
        />
      ) : null}

      {detailDevice !== null ? (
        <InfraDeviceDetailModal
          device={detailDevice}
          siteOptions={resolvedSiteOptions}
          buildingOptions={resolvedBuildingOptions}
          canManage={canManage}
          onSave={onUpdateDevice}
          onDelete={onDeleteDevice}
          onDismiss={() => setDetailDeviceId(null)}
        />
      ) : null}

      {showAddModal ? (
        <AddInfraDeviceModal
          siteOptions={resolvedSiteOptions}
          buildingOptions={resolvedBuildingOptions}
          initialSiteName={initialSiteName}
          onDismiss={() => setShowAddModal(false)}
          onSubmit={handleCreateDevice}
        />
      ) : null}

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
