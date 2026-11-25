import { useCallback, useEffect, useMemo, useRef } from "react";
import { Outlet, useLocation, useNavigate, useParams, useSearchParams } from "react-router-dom";

import ModuleActionBar from "./ModuleActionBar";
import ModuleActionDialog from "./ModuleActionDialog";
import { moduleColConfig, moduleColTitles } from "./moduleColumns";
import { matchesModuleFilters, moduleFilters, parseModuleListState, writeModuleFilters } from "./moduleListFilters";
import {
  buildModuleRows,
  compareModuleRows,
  moduleColumns,
  type ModuleListItem,
  moduleName,
  sortableModuleColumns,
} from "./moduleListModel";
import ModuleName from "./ModuleName";
import { useModuleActions } from "./useModuleActions";
import type { TelemetryLevel } from "@/protoOS/api/adapters/types";
import { useTelemetry } from "@/protoOS/api/hooks/useTelemetry";
import ListContentInset from "@/protoOS/components/ContentLayout/ListContentInset";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { modulePoolTarget } from "@/protoOS/features/poolAssignment/targets";
import { usePoolAssignmentActions } from "@/protoOS/features/poolAssignment/usePoolAssignmentActions";
import ModuleModal from "@/protoOS/features/tanks/components/ModuleModal/ModuleModal";
import useMinerStore from "@/protoOS/store/useMinerStore";
import {
  findModuleInTankByRouteParam,
  findTankByRouteParam,
  moduleRouteValue,
  tankRouteValue,
} from "@/protoOS/topologyRouteParams";
import Button from "@/shared/components/Button";
import List from "@/shared/components/List";
import type { ActiveFilters } from "@/shared/components/List/Filters/types";

const LIST_PADDING = { phone: "24px", tablet: "40px", laptop: "56px", desktop: "56px" };

const TELEMETRY_LEVELS: TelemetryLevel[] = ["hashboard", "miner"];

const Modules = () => {
  const { tankId, moduleId, serial } = useParams();
  const navigate = useNavigate();
  const { search } = useLocation();
  const [params, setParams] = useSearchParams();
  const { minerRoot } = useMinerHosting();
  const topology = useMinerStore((state) => state.hardware.topology);
  const resource = useMinerStore((state) => state.hardware.resource);
  const telemetry = useMinerStore((state) => state.telemetry);
  const errorsByOwner = useMinerStore((state) => state.minerStatus.errorsByOwner);
  const generation = useMinerStore((state) => state.connectionGeneration);
  const { error } = useTelemetry({ level: TELEMETRY_LEVELS, enabled: !serial });
  const listPath = `${minerRoot}/modules`;
  const rowElements = useRef(new Map<string, HTMLTableRowElement>());
  const lastOpened = useRef<string | undefined>(undefined);
  const heading = useRef<HTMLHeadingElement>(null);
  const { filters, sort } = useMemo(() => parseModuleListState(params), [params]);
  const rows = useMemo(
    () =>
      topology?.kind === "container" ? buildModuleRows(topology, telemetry, [...errorsByOwner.values()].flat()) : [],
    [topology, telemetry, errorsByOwner],
  );
  const actions = useModuleActions(rows, topology);
  const poolActions = usePoolAssignmentActions();
  const actionsFor = (ids: string[]) => [
    ...actions.actionsFor(ids),
    ...poolActions.action(topology?.kind === "container" ? modulePoolTarget(topology, ids) : undefined, "module"),
  ];
  const filterOptions = useMemo(() => moduleFilters(rows), [rows]);
  const visibleRows = useMemo(
    () =>
      rows
        .filter((row) => matchesModuleFilters(row, filters))
        .sort((a, b) => compareModuleRows(a, b, sort.field, sort.direction)),
    [rows, filters, sort],
  );
  const hasFilters = Object.values(filters.dropdownFilters).some((values) => values.length > 0);
  const onFilter = useCallback(
    async (nextFilters: ActiveFilters) => {
      const next = writeModuleFilters(params, nextFilters);
      if (next.toString() !== params.toString()) setParams(next);
    },
    [params, setParams],
  );
  const tank = findTankByRouteParam(topology, tankId);
  const module = tank && topology ? findModuleInTankByRouteParam(topology, tank, moduleId) : undefined;
  const close = () => navigate({ pathname: listPath, search });
  const openModule = (row: ModuleListItem) => {
    if (!row.tank) return;
    lastOpened.current = row.id;
    rowElements.current.get(row.id)?.focus({ preventScroll: true });
    navigate({
      pathname: `${listPath}/${tankRouteValue(row.tank)}/${moduleRouteValue(row.module, row.tank.moduleIds.indexOf(row.module.id) + 1)}`,
      search,
    });
  };
  useEffect(() => {
    if (!moduleId && lastOpened.current) {
      (rowElements.current.get(lastOpened.current) ?? heading.current)?.focus({ preventScroll: true });
      lastOpened.current = undefined;
    }
  }, [moduleId]);

  if (!topology && (resource.status === "loading" || resource.status === "idle"))
    return (
      <p role="status" className="p-6 tablet:p-10 laptop:p-14">
        Loading modules…
      </p>
    );
  if (topology?.kind !== "container")
    return (
      <p role="alert" className="p-6 tablet:p-10 laptop:p-14">
        Unable to load modules.
      </p>
    );
  return (
    <>
      {poolActions.dialog}
      <section
        aria-label="Module inventory"
        inert={!!moduleId || !!actions.request || poolActions.open}
        className="w-max min-w-full pt-6 pb-28 laptop:pt-10"
      >
        <ListContentInset>
          <h1 ref={heading} tabIndex={-1} className="mb-4 text-heading-300">
            Modules
          </h1>
          {error ? (
            <p role="alert" className="mb-4 text-300 text-text-critical">
              Could not refresh module readings. Displayed values may be out of date.
            </p>
          ) : null}
        </ListContentInset>
        <List
          key={generation}
          itemSelectable
          pageScopedSelection
          rowSelectionLabel={(row) => `Select ${row.name}`}
          paddingLeft={LIST_PADDING}
          paddingRight={LIST_PADDING}
          overflowContainer="page"
          scrollLeadingPadding
          stickyChromeClassName="w-[100cqw] pr-6 tablet:pr-10 laptop:pr-14"
          activeCols={moduleColumns}
          colTitles={moduleColTitles}
          colConfig={{
            ...moduleColConfig,
            name: {
              ...moduleColConfig.name!,
              component: (row) => (
                <ModuleName row={row} actions={actionsFor([row.id])} onViewErrors={() => openModule(row)} />
              ),
            },
          }}
          items={visibleRows}
          itemKey="id"
          itemName={{ singular: "module", plural: "modules" }}
          total={visibleRows.length}
          totalUnfiltered={rows.length}
          hasActiveFilters={hasFilters}
          filters={filterOptions}
          initialActiveFilters={filters}
          onServerFilter={onFilter}
          sortableColumns={sortableModuleColumns}
          currentSort={sort}
          onSort={(field, direction) => {
            const next = new URLSearchParams(params);
            next.set("sort", field);
            next.set("direction", direction);
            setParams(next);
          }}
          getDefaultSortDirection={(field) =>
            field === "hashrate" || field === "issues" || field === "temperature" || field === "power" ? "desc" : "asc"
          }
          renderActionBar={(selected, clear) => (
            <ModuleActionBar
              selected={selected}
              clear={clear}
              actions={actionsFor(selected)}
              hidden={!!moduleId || !!actions.request || poolActions.open}
            />
          )}
          onRowClick={openModule}
          itemRef={(id, element) => {
            if (element) rowElements.current.set(id, element);
            else rowElements.current.delete(id);
          }}
          noDataElement={
            <div role="status" className="py-12 text-center text-text-primary-70">
              {rows.length ? "No modules match these filters." : "No modules found."}
              {hasFilters ? (
                <div className="mt-4">
                  <Button
                    variant="secondary"
                    text="Clear filters"
                    onClick={() => void onFilter(parseModuleListState(new URLSearchParams()).filters)}
                  />
                </div>
              ) : null}
            </div>
          }
        />
      </section>
      {moduleId && (!module || !tank) ? (
        <div role="status" className="mt-6">
          Module not found. <Button variant="secondary" text="Back to modules" onClick={close} />
        </div>
      ) : null}
      {module && tank && !serial ? (
        <ModuleModal
          module={module}
          tank={tank}
          topology={topology}
          onClose={close}
          modulePath={(item) =>
            `${listPath}/${tankRouteValue(tank)}/${moduleRouteValue(item, tank.moduleIds.indexOf(item.id) + 1)}`
          }
          moduleLabel={(item) => moduleName(item, tank)}
        />
      ) : null}
      <ModuleActionDialog controls={actions} />
      <Outlet />
    </>
  );
};

export default Modules;
