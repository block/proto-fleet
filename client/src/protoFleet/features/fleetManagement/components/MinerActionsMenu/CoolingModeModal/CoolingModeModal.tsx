import React, { useEffect, useState } from "react";
import clsx from "clsx";
import { CoolingMode } from "@/protoFleet/api/generated/common/v1/cooling_pb";
import { Fan } from "@/shared/assets/icons";
import Immersion from "@/shared/assets/icons/Immersion";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal/Modal";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";
import { COOLING_MODES, type CoolingModeOption } from "@/shared/constants/cooling";

interface CoolingModeModalProps {
  open?: boolean;
  minerCount: number;
  initialCoolingMode?: CoolingMode;
  onConfirm: (coolingMode: CoolingMode) => void;
  onDismiss: () => void;
}

interface CoolingOptionProps {
  title: string;
  description: string;
  icon: React.ReactNode;
  isSelected: boolean;
}

const CoolingOption = ({ title, description, icon, isSelected }: CoolingOptionProps) => (
  <div className="flex items-center justify-start gap-4">
    <div
      className={clsx("flex h-8 w-8 items-center justify-center rounded-lg", {
        "bg-core-primary-5": isSelected,
        "bg-surface-5": !isSelected,
      })}
    >
      {icon}
    </div>
    <div className="flex flex-col gap-1">
      <div className="text-300 font-medium text-text-primary">{title}</div>
      <div className="text-text-secondary text-200">{description}</div>
    </div>
  </div>
);

interface CoolingModeConfig {
  id: CoolingModeOption;
  testId: string;
  title: string;
  description: string;
  icon: React.ReactNode;
  coolingMode: CoolingMode;
}

const COOLING_OPTIONS: CoolingModeConfig[] = [
  {
    id: COOLING_MODES.air,
    testId: "cooling-option-air",
    title: "Air cooled",
    description: "Your fans will be used to cool your miner",
    icon: <Fan />,
    coolingMode: CoolingMode.AIR_COOLED,
  },
  {
    id: COOLING_MODES.immersion,
    testId: "cooling-option-immersion",
    title: "Immersion cooled",
    description: "Your fans will be disabled",
    icon: <Immersion />,
    coolingMode: CoolingMode.IMMERSION_COOLED,
  },
];

const coolingModeToOption = (mode: CoolingMode | undefined): CoolingModeOption | undefined => {
  switch (mode) {
    case CoolingMode.AIR_COOLED:
      return COOLING_MODES.air;
    case CoolingMode.IMMERSION_COOLED:
      return COOLING_MODES.immersion;
    case CoolingMode.MANUAL:
    case CoolingMode.UNSPECIFIED:
    default:
      return undefined;
  }
};

const CoolingModeModal = ({ open, minerCount, initialCoolingMode, onConfirm, onDismiss }: CoolingModeModalProps) => {
  const [selectedOption, setSelectedOption] = useState<CoolingModeOption | undefined>(
    coolingModeToOption(initialCoolingMode),
  );

  // Sync state with prop when initialCoolingMode changes
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- sync local selection with controlled initialCoolingMode prop
    setSelectedOption(coolingModeToOption(initialCoolingMode));
  }, [initialCoolingMode]);

  const handleConfirm = () => {
    if (!selectedOption) return;

    const selected = COOLING_OPTIONS.find((option) => option.id === selectedOption);
    if (selected) {
      onConfirm(selected.coolingMode);
    }
    setSelectedOption(undefined);
  };

  const handleDismiss = () => {
    setSelectedOption(undefined);
    onDismiss();
  };

  const handleChange = (id: string) => {
    setSelectedOption(id as CoolingModeOption);
  };

  const minerText = minerCount === 1 ? "miner" : "miners";
  const hasSelection = selectedOption !== undefined;

  return (
    <Modal
      open={open}
      title="Set cooling mode"
      onDismiss={handleDismiss}
      buttons={[
        {
          text: hasSelection ? "Update cooling mode" : "Done",
          variant: variants.primary,
          onClick: hasSelection ? handleConfirm : handleDismiss,
        },
      ]}
      divider={false}
    >
      <div className="text-text-secondary mb-6 text-200">{`Update the cooling mode for ${minerCount} ${minerText}`}</div>
      <div className="flex flex-col gap-4">
        {COOLING_OPTIONS.map((option) => (
          <SelectRow
            key={option.id}
            id={option.id}
            data-testid={option.testId}
            isSelected={selectedOption === option.id}
            onChange={handleChange}
            divider={false}
            className={clsx("border-1 border-border-5", {
              "border-border-20": selectedOption === option.id,
            })}
            text={
              <CoolingOption
                title={option.title}
                description={option.description}
                icon={option.icon}
                isSelected={selectedOption === option.id}
              />
            }
            type={selectTypes.radio}
          />
        ))}
      </div>
    </Modal>
  );
};

export default CoolingModeModal;
