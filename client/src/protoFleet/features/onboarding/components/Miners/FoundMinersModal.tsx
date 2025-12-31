import { Dispatch, SetStateAction, useCallback, useMemo } from "react";
import type { MinerWithSelected, MinerWithSelectedAndAction } from "./types";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

import List from "@/shared/components/List";
import { ActiveFilters, DropdownFilterItem } from "@/shared/components/List/Filters/types";
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
  miners: MinerWithSelected[];
  models: string[];
  setDeselectedMiners: Dispatch<SetStateAction<Device["deviceIdentifier"][]>>;
  onDismiss: () => void;
};

const FoundMinersModal = ({ miners, models, setDeselectedMiners, onDismiss }: FoundMinersModalProps) => {
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

  const modelFilter = useMemo(() => {
    const options = models.map((model) => ({
      id: model,
      label: model,
    }));

    return {
      type: "dropdown",
      title: "Model",
      value: "model",
      options: [...options],
      defaultOptionIds: [...options.map((o) => o.id)],
    } as DropdownFilterItem;
  }, [models]);

  const filterItem = useCallback((item: MinerWithSelectedAndAction, filters: ActiveFilters) => {
    const modelFilters = filters.dropdownFilters?.["model"];

    // If no model filter is applied (empty array or undefined), show all items
    if (!modelFilters || modelFilters.length === 0) {
      return true;
    }

    // If model filters are applied, only show items that match
    if (!modelFilters.includes(item.model)) {
      return false;
    }

    return true;
  }, []);

  return (
    <Modal
      onDismiss={onDismiss}
      size="large"
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: variants.primary,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <Header
          titleSize="text-heading-300"
          title={`${miners.length} miners found on your network`}
          subtitle="Selected miners will be added to your fleet."
        />
        <List<MinerWithSelectedAndAction, MinerWithSelectedAndAction["deviceIdentifier"]>
          filters={[modelFilter]}
          filterItem={filterItem}
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
        onSelectAll={() => setSelectedMiners(miners.map((miner) => miner.deviceIdentifier))}
        onSelectNone={() => setSelectedMiners([])}
      />
    </Modal>
  );
};

export default FoundMinersModal;
