import { useState } from "react";

import { variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Select from "@/shared/components/Select";

interface DeviceSettingsModalProps {
  onDismiss: () => void;
}

const SEQUENCE_OPTIONS = [
  { value: "before_miners", label: "Before miners" },
  { value: "with_miners", label: "With miners" },
  { value: "after_miners", label: "After miners" },
];

const MODE_OPTIONS = [
  { value: "fullFleet", label: "Full shutdown" },
  { value: "fixedKwReduction", label: "Fixed kW reduction" },
];

const DeviceSettingsModal = ({ onDismiss }: DeviceSettingsModalProps) => {
  const [curtailSequence, setCurtailSequence] = useState("with_miners");
  const [curtailOffsetSec, setCurtailOffsetSec] = useState("0");
  const [curtailMode, setCurtailMode] = useState("fullFleet");
  const [curtailTargetKw, setCurtailTargetKw] = useState("500");

  const [restoreSequence, setRestoreSequence] = useState("after_miners");
  const [restoreOffsetSec, setRestoreOffsetSec] = useState("5");

  const curtailOffsetDisabled = curtailSequence === "with_miners";
  const curtailTargetDisabled = curtailMode !== "fixedKwReduction";
  const restoreOffsetDisabled = restoreSequence === "with_miners";

  const curtailOffsetDisplay = curtailOffsetDisabled ? "0" : curtailOffsetSec;
  const curtailTargetDisplay = curtailTargetDisabled ? "100%" : curtailTargetKw;
  const restoreOffsetDisplay = restoreOffsetDisabled ? "0" : restoreOffsetSec;

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title="Fan behavior"
      description="Prefilled from response profile. Override to customize fan-specific behavior."
      buttons={[
        {
          text: "Save",
          variant: variants.primary,
          onClick: () => onDismiss(),
        },
      ]}
    >
      <div className="flex flex-col gap-6">
        <div className="flex flex-col gap-3">
          <span className="text-300 font-medium text-text-primary">Curtail</span>
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="curtail-sequence"
              label="Sequence"
              options={SEQUENCE_OPTIONS}
              value={curtailSequence}
              onChange={setCurtailSequence}
              forceBelow
            />
            <Input
              id="curtail-offset"
              label="Offset (seconds)"
              initValue={curtailOffsetDisplay}
              onChange={(v) => setCurtailOffsetSec(v)}
              type={curtailOffsetDisabled ? "text" : "number"}
              disabled={curtailOffsetDisabled}
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="curtail-mode"
              label="Mode"
              options={MODE_OPTIONS}
              value={curtailMode}
              onChange={setCurtailMode}
              forceBelow
            />
            <Input
              id="curtail-target"
              label="Target (kW)"
              initValue={curtailTargetDisplay}
              onChange={(v) => setCurtailTargetKw(v)}
              type={curtailTargetDisabled ? "text" : "number"}
              disabled={curtailTargetDisabled}
            />
          </div>
        </div>

        <Divider />

        <div className="flex flex-col gap-3">
          <span className="text-300 font-medium text-text-primary">Restore</span>
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="restore-sequence"
              label="Sequence"
              options={SEQUENCE_OPTIONS}
              value={restoreSequence}
              onChange={setRestoreSequence}
              forceBelow
            />
            <Input
              id="restore-offset"
              label="Offset (seconds)"
              initValue={restoreOffsetDisplay}
              onChange={(v) => setRestoreOffsetSec(v)}
              type={restoreOffsetDisabled ? "text" : "number"}
              disabled={restoreOffsetDisabled}
            />
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default DeviceSettingsModal;
