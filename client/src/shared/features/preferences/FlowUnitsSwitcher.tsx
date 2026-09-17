import clsx from "clsx";
import type { FlowUnit } from "./types";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import SelectRow from "@/shared/components/SelectRow";
import { selectTypes } from "@/shared/constants";

const options: { unit: FlowUnit; text: string }[] = [
  { unit: "GPM", text: "US gallons per minute (GPM)" },
  { unit: "L/min", text: "Liters per minute (L/min)" },
];

const FlowUnitsSwitcher = ({
  onClickDone,
  flowUnit,
  setFlowUnit,
}: {
  onClickDone: () => void;
  flowUnit: FlowUnit;
  setFlowUnit: (unit: FlowUnit) => void;
}) => (
  <Modal
    title="Flow"
    dialogLabel="Flow"
    manageFocus
    onDismiss={onClickDone}
    buttons={[{ text: "Done", onClick: onClickDone, variant: variants.secondary }]}
    divider={false}
  >
    <div className="mt-6 flex flex-col gap-4">
      {options.map(({ unit, text }) => (
        <SelectRow
          key={unit}
          id={unit}
          text={text}
          isSelected={flowUnit === unit}
          onChange={(_, selected) => {
            if (selected) setFlowUnit(unit);
          }}
          divider={false}
          className={clsx("border-1 border-border-5", { "border-border-20": flowUnit === unit })}
          type={selectTypes.radio}
        />
      ))}
    </div>
  </Modal>
);

export default FlowUnitsSwitcher;
