import { Dispatch, SetStateAction, useCallback, useMemo, useState } from "react";
import { fleetSourceLabel, foundSummary } from "./foundMinersLabels";
import { createFoundMinersModelFilter, filterFoundMinerByModel } from "./foundMinersModelFilter";
import { DeviceClass } from "@/protoFleet/api/generated/common/v1/common_pb";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { type FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import ListPagination from "@/protoFleet/components/ListPagination";
import { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import Filters from "@/shared/components/List/Filters";
import { ActiveFilters } from "@/shared/components/List/Filters/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";

type FoundMinerRow = Device & { source: string };

const PAGE_SIZE = 100;

const activeCols = ["model", "ipAddress", "source"] as (keyof FoundMinerRow)[];

const minerColTitles = {
  model: "Model",
  ipAddress: "IP address",
  source: "Source",
} as {
  [key in (typeof activeCols)[number]]: string;
};

const colConfig = {
  source: { width: "w-full pr-3" },
  model: {
    width: "w-full pr-10",
    // Relabel a container row's model cell to "Proto Container" so no raw code
    // name ("CU1") leaks into the discovery table, matching the group label and
    // the model filter option. Non-container rows keep their raw model string.
    // Filtering/sorting still key off the untouched `miner.model` field.
    component: (item: FoundMinerRow) => (item.deviceClass === DeviceClass.CONTAINER ? "Proto Container" : item.model),
  },
  ipAddress: {
    width: "w-full pr-10",
  },
};

type FoundMinersModalProps = {
  open?: boolean;
  miners: Device[];
  deselectedMinerIds: ReadonlySet<string>;
  fleetNodes?: FleetNodeItem[];
  setDeselectedMiners: Dispatch<SetStateAction<Device["deviceIdentifier"][]>>;
  onDismiss: () => void;
};

const FoundMinersModal = ({
  open,
  miners,
  deselectedMinerIds,
  fleetNodes,
  setDeselectedMiners,
  onDismiss,
}: FoundMinersModalProps) => {
  const [page, setPage] = useState(0);
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>({
    buttonFilters: [],
    dropdownFilters: {},
    numericFilters: {},
    textareaListFilters: {},
  });

  const selectedCount = useMemo(
    () => miners.reduce((count, miner) => count + Number(!deselectedMinerIds.has(miner.deviceIdentifier)), 0),
    [miners, deselectedMinerIds],
  );

  const updateSelection = useCallback(
    (ids: Set<string>, isSelected: (id: string) => boolean) => {
      setDeselectedMiners((previous) => {
        const next = new Set(previous);
        for (const id of ids) {
          if (isSelected(id)) next.delete(id);
          else next.add(id);
        }
        return [...next];
      });
    },
    [setDeselectedMiners],
  );

  const modelFilter = useMemo(() => createFoundMinersModelFilter(miners), [miners]);

  const filteredMiners = useMemo(() => {
    return miners.filter((miner) => filterFoundMinerByModel(miner, activeFilters));
  }, [miners, activeFilters]);

  const currentPage = Math.min(page, Math.max(0, Math.ceil(filteredMiners.length / PAGE_SIZE) - 1));
  const pageStart = currentPage * PAGE_SIZE;
  const pageMiners = useMemo(() => {
    const nodes = new Map(fleetNodes?.map((node) => [node.fleetNodeId, node.name]));
    return filteredMiners.slice(pageStart, pageStart + PAGE_SIZE).map((miner) => {
      const id = miner.fleetNodeId.toString();
      return { ...miner, source: fleetSourceLabel(id, nodes.get(id)) };
    });
  }, [filteredMiners, fleetNodes, pageStart]);
  const filteredIds = useMemo(() => new Set(filteredMiners.map((miner) => miner.deviceIdentifier)), [filteredMiners]);
  const deviceGroups = [
    { label: "Containers", items: pageMiners.filter((device) => device.deviceClass === DeviceClass.CONTAINER) },
    { label: "Miners", items: pageMiners.filter((device) => device.deviceClass !== DeviceClass.CONTAINER) },
  ];

  // Split the discovered total by entity so the modal header matches the inline
  // FoundMiners summary ("N containers and M miners found on your network").
  const summaryTitle = useMemo(() => {
    const containerCount = miners.reduce(
      (total, miner) => total + (miner.deviceClass === DeviceClass.CONTAINER ? 1 : 0),
      0,
    );
    return foundSummary(containerCount, miners.length - containerCount);
  }, [miners]);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      size="large"
      divider={false}
      title={summaryTitle}
      description="Selected devices will be added to your fleet."
      className="flex !h-[calc(100dvh-(--spacing(32)))] max-h-[calc(100dvh-(--spacing(32)))] flex-col !overflow-hidden phone:!h-[calc(100dvh-theme(spacing.10))] phone:max-h-[calc(100dvh-theme(spacing.10))]"
      bodyClassName="flex min-h-0 flex-1 flex-col"
      buttons={[
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex h-full min-h-0 flex-col">
        <Filters
          filterItems={[modelFilter]}
          items={miners}
          onFilter={(filters) => {
            if (filters !== activeFilters) {
              setActiveFilters(filters);
              setPage(0);
            }
          }}
          filterSize={sizes.compact}
        />
        <div className="min-h-0 flex-1 overflow-y-auto">
          {deviceGroups.map(({ label, items: group }) => {
            if (!group.length) return null;
            const groupIds = new Set(group.map((device) => device.deviceIdentifier));
            return (
              <section key={label} aria-label={label}>
                <h3 className="px-3 py-2 text-emphasis-300">{label}</h3>
                <List<FoundMinerRow, FoundMinerRow["deviceIdentifier"]>
                  activeCols={activeCols}
                  colTitles={minerColTitles}
                  colConfig={colConfig}
                  items={group}
                  itemKey="deviceIdentifier"
                  itemSelectable
                  preserveOffPageSelection
                  customSelectedItems={group
                    .filter((miner) => !deselectedMinerIds.has(miner.deviceIdentifier))
                    .map((miner) => miner.deviceIdentifier)}
                  customSetSelectedItems={(selected) => {
                    const selectedIds = new Set(selected);
                    updateSelection(groupIds, (id) => selectedIds.has(id));
                  }}
                  containerClassName="min-h-0"
                  tableClassName="mb-0"
                  overflowContainer={true}
                  stickyBgColor="bg-surface-elevated-base"
                />
              </section>
            );
          })}
        </div>
        <ListPagination
          firstItemIndex={pageStart + 1}
          lastItemIndex={Math.min(pageStart + PAGE_SIZE, filteredMiners.length)}
          total={filteredMiners.length}
          itemName="devices"
          hasPreviousPage={currentPage > 0}
          hasNextPage={pageStart + PAGE_SIZE < filteredMiners.length}
          onPrevious={() => setPage(currentPage - 1)}
          onNext={() => setPage(currentPage + 1)}
          className="py-3"
        />
        <ModalSelectAllFooter
          label={`${selectedCount} ${selectedCount === 1 ? "device" : "devices"} selected`}
          onSelectAll={() => updateSelection(filteredIds, () => true)}
          onSelectNone={() => updateSelection(filteredIds, () => false)}
        />
      </div>
    </Modal>
  );
};

export default FoundMinersModal;
