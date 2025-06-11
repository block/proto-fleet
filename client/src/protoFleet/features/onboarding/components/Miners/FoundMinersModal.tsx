import { Dispatch, SetStateAction, useCallback, useMemo } from "react";
import type { MinerWithSelected, MinerWithSelectedAndAction } from "./types";
import { Device } from "@/protoFleet/api/generated/pairing/v1/pairing_pb";
import { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";

import List from "@/shared/components/List";
import { DropdownFilterItem } from "@/shared/components/List/Filters/types";
import Modal from "@/shared/components/Modal";

const activeCols = [
  "model",
  "serialNumber",
  "macAddress",
] as (keyof MinerWithSelectedAndAction)[];

const minerColTitles = {
  model: "Model",
  serialNumber: "Control board serial",
  macAddress: "MAC address",
} as {
  [key in (typeof activeCols)[number]]: string;
};

const colConfig = {
  model: {
    width: "w-full pr-10",
  },
  serialNumber: {
    width: "w-full pr-10",
  },
  macAddress: {
    width: "w-full pr-10",
  },
};

type FoundMinersModalProps = {
  miners: MinerWithSelected[];
  models: string[];
  setDeselectedMiners: Dispatch<SetStateAction<Device["deviceIdentifier"][]>>;
  onDismiss: () => void;
};

const FoundMinersModal = ({
  miners,
  models,
  setDeselectedMiners,
  onDismiss,
}: FoundMinersModalProps) => {
  const selectedMiners = useMemo(() => {
    return miners
      .filter((miner) => miner.selected)
      .map((miner) => miner.deviceIdentifier);
  }, [miners]);

  // Since were keeping deslected miners as state in parent component
  // we need to define a a setSelectedMiners function that will update
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

  const blinkAction = {
    title: "Blink LEDs",
    actionHandler: (miner: Device) => {
      // TODO: call API to blink LEDs
      // eslint-disable-next-line
      console.log("Blink LEDs for miner:", miner);
    },
  };

  const modelFilter = useMemo(() => {
    const options = models.map((model) => ({
      id: model,
      label: model,
    }));

    return {
      type: "dropdown",
      title: "Model",
      value: "model",
      options: [{ id: "all", label: "All Models" }, ...options],
      defaultOptionId: "all",
    } as DropdownFilterItem<"model">;
  }, [models]);

  const filterItem = useCallback(
    (
      item: MinerWithSelectedAndAction,
      _: ("model" | "all")[],
      dropdownFilters?: Record<string, string>,
    ) => {
      if (
        dropdownFilters &&
        dropdownFilters["model"] &&
        dropdownFilters["model"] !== "all"
      ) {
        if (item.model !== dropdownFilters["model"]) {
          return false;
        }
      }
      return true;
    },
    [],
  );

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
      <div className="flex flex-col gap-4 overflow-hidden">
        <Header
          titleSize="text-heading-300"
          title={`${miners.length} miners found on your network`}
          subtitle="Selected miners will be added to your fleet."
        />
        <List<
          MinerWithSelectedAndAction,
          MinerWithSelectedAndAction["deviceIdentifier"],
          "model"
        >
          filters={[modelFilter]}
          filterItem={filterItem}
          filterSize={sizes.base}
          activeCols={activeCols}
          colTitles={minerColTitles}
          colConfig={colConfig}
          items={miners}
          itemKey="deviceIdentifier"
          itemSelectable
          customSelectedItems={selectedMiners}
          customSetSelectedItems={setSelectedMiners}
          actions={[blinkAction]}
          containerClassName="max-h-[50vh]"
        />
      </div>
    </Modal>
  );
};

export default FoundMinersModal;
