import { Dispatch, SetStateAction, useCallback, useMemo, useState } from "react";
import type { MinerWithSelected, MinerWithSelectedAndAction } from "./types";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { createModelFilter, filterByModel } from "@/protoFleet/utils/minerFilters";
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

  const modelFilter = useMemo(() => createModelFilter(models), [models]);

  const filteredMiners = useMemo(() => {
    return miners.filter((miner) => filterByModel(miner, activeFilters));
  }, [miners, activeFilters]);

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      size="large"
      divider={false}
      title={`${miners.length} miners found on your network`}
      description="Selected miners will be added to your fleet."
      buttons={[
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <List<MinerWithSelectedAndAction, MinerWithSelectedAndAction["deviceIdentifier"]>
          filters={[modelFilter]}
          filterItem={filterByModel}
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
          containerClassName="max-h-[50vh]"
          overflowContainer={true}
          stickyBgColor="bg-surface-elevated-base"
        />
      </div>
      <ModalSelectAllFooter
        label={selectedMiners.length + " miners selected"}
        onSelectAll={() => setSelectedMiners(filteredMiners.map((miner) => miner.deviceIdentifier))}
        onSelectNone={() => setSelectedMiners([])}
      />
    </Modal>
  );
};

export default FoundMinersModal;
