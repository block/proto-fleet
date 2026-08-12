import { Dispatch, SetStateAction, useCallback, useMemo, useState } from "react";
import { foundSummary, isContainerModel } from "./foundMinersLabels";
import { createFoundMinersModelFilter, filterFoundMinerByModel } from "./foundMinersModelFilter";
import type { MinerWithSelected, MinerWithSelectedAndAction } from "./types";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { ActiveFilters } from "@/shared/components/List/Filters/types";
import Modal, { ModalSelectAllFooter } from "@/shared/components/Modal";

const activeCols = ["model", "ipAddress"] as (keyof MinerWithSelectedAndAction)[];

const minerColTitles = {
  model: "Model",
  ipAddress: "IP address",
} as {
  [key in (typeof activeCols)[number]]: string;
};

const colConfig = {
  model: {
    width: "w-full pr-10",
    // Relabel a container row's model cell to "Proto Container" so no raw code
    // name ("CU1") leaks into the discovery table, matching the group label and
    // the model filter option. Non-container rows keep their raw model string.
    // Filtering/sorting still key off the untouched `miner.model` field.
    component: (item: MinerWithSelectedAndAction) => (isContainerModel(item.model) ? "Proto Container" : item.model),
  },
  ipAddress: {
    width: "w-full pr-10",
  },
};

type FoundMinersModalProps = {
  open?: boolean;
  miners: MinerWithSelected[];
  models: string[];
  setDeselectedMiners: Dispatch<SetStateAction<Device["deviceIdentifier"][]>>;
  onDismiss: () => void;
};

const FoundMinersModal = ({ open, miners, models, setDeselectedMiners, onDismiss }: FoundMinersModalProps) => {
  const [activeFilters, setActiveFilters] = useState<ActiveFilters>({
    buttonFilters: [],
    dropdownFilters: {},
    numericFilters: {},
    textareaListFilters: {},
  });

  const selectedMiners = useMemo(() => {
    return miners.filter((miner) => miner.selected).map((miner) => miner.deviceIdentifier);
  }, [miners]);

  // Since were keeping deslected miners as state in parent component
  // we need to define a setSelectedMiners function that will update
  // the deselected miners based on the selected miners
  const setSelectedMiners = useCallback(
    (selected: MinerWithSelected["deviceIdentifier"][]) => {
      const deselected = miners
        .filter((miner) => !selected.includes(miner.deviceIdentifier))
        .map((miner) => miner.deviceIdentifier);

      setDeselectedMiners(deselected);
    },
    [miners, setDeselectedMiners],
  );

  const modelFilter = useMemo(() => createFoundMinersModelFilter(models), [models]);

  const filteredMiners = useMemo(() => {
    return miners.filter((miner) => filterFoundMinerByModel(miner, activeFilters));
  }, [miners, activeFilters]);

  // Split the discovered total by entity so the modal header matches the inline
  // FoundMiners summary ("N containers and M miners found on your network").
  const summaryTitle = useMemo(() => {
    const containerCount = miners.reduce((total, miner) => total + (isContainerModel(miner.model) ? 1 : 0), 0);
    return foundSummary(containerCount, miners.length - containerCount);
  }, [miners]);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      size="large"
      divider={false}
      title={summaryTitle}
      description="Selected miners will be added to your fleet."
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
        <div className="min-h-0 flex-1 overflow-y-auto">
          <List<MinerWithSelectedAndAction, MinerWithSelectedAndAction["deviceIdentifier"]>
            filters={[modelFilter]}
            filterItem={filterFoundMinerByModel}
            onFilterChange={setActiveFilters}
            filterSize={sizes.compact}
            activeCols={activeCols}
            colTitles={minerColTitles}
            colConfig={colConfig}
            items={miners}
            itemKey="deviceIdentifier"
            itemSelectable
            customSelectedItems={selectedMiners}
            customSetSelectedItems={setSelectedMiners}
            containerClassName="min-h-0"
            tableClassName="mb-0"
            overflowContainer={true}
            stickyBgColor="bg-surface-elevated-base"
          />
        </div>
        <ModalSelectAllFooter
          label={selectedMiners.length + " miners selected"}
          onSelectAll={() => setSelectedMiners(filteredMiners.map((miner) => miner.deviceIdentifier))}
          onSelectNone={() => setSelectedMiners([])}
        />
      </div>
    </Modal>
  );
};

export default FoundMinersModal;
