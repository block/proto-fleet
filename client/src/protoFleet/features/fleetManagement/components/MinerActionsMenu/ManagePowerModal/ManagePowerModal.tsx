import { useState } from "react";
import clsx from "clsx";
import { PerformanceMode } from "@/protoFleet/api/generated/minercommand/v1/command_pb";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal/Modal";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";

interface ManagePowerModalProps {
  open?: boolean;
  onConfirm: (performanceMode: PerformanceMode) => void;
  onDismiss: () => void;
}

interface PowerOptionProps {
  title: string;
  description: string;
}

const PowerOption = ({ title, description }: PowerOptionProps) => (
  <div className="flex flex-col gap-1">
    <div className="text-300 font-medium text-text-primary">{title}</div>
    <div className="text-text-secondary text-200">{description}</div>
  </div>
);

const POWER_MODES = {
  maximize: "maximize",
  reduce: "reduce",
} as const;

type PowerMode = (typeof POWER_MODES)[keyof typeof POWER_MODES];

const ManagePowerModal = ({ open, onConfirm, onDismiss }: ManagePowerModalProps) => {
  const [selectedOption, setSelectedOption] = useState<PowerMode | undefined>(undefined);

  const handleConfirm = () => {
    if (!selectedOption) return;

    if (selectedOption === POWER_MODES.maximize) {
      onConfirm(PerformanceMode.MAXIMUM_HASHRATE);
    } else {
      onConfirm(PerformanceMode.EFFICIENCY);
    }
    setSelectedOption(undefined);
  };

  const handleDismiss = () => {
    setSelectedOption(undefined);
    onDismiss();
  };

  const handleChange = (id: string) => {
    setSelectedOption(id as PowerMode);
  };

  return (
    <Modal
      open={open}
      title="Manage power"
      onDismiss={handleDismiss}
      buttons={[
        {
          text: "Confirm",
          variant: variants.primary,
          onClick: handleConfirm,
        },
      ]}
      divider={false}
    >
      <div className="mt-6 flex flex-col gap-4">
        <SelectRow
          id={POWER_MODES.maximize}
          data-testid="power-option-maximize"
          isSelected={selectedOption === POWER_MODES.maximize}
          onChange={handleChange}
          divider={false}
          className={clsx("border-1 border-border-5", {
            "border-border-20": selectedOption === POWER_MODES.maximize,
          })}
          text={
            <PowerOption
              title="Maximize power"
              description="Push each miner to the upper end of its power range for peak hashrate output."
            />
          }
          type={selectTypes.radio}
        />
        <SelectRow
          id={POWER_MODES.reduce}
          data-testid="power-option-reduce"
          isSelected={selectedOption === POWER_MODES.reduce}
          onChange={handleChange}
          divider={false}
          className={clsx("border-1 border-border-5", {
            "border-border-20": selectedOption === POWER_MODES.reduce,
          })}
          text={
            <PowerOption
              title="Reduce power"
              description="Limit each miner to the lower end of its power range to conserve energy and lower costs."
            />
          }
          type={selectTypes.radio}
        />
      </div>
    </Modal>
  );
};

export default ManagePowerModal;
