import { useState } from "react";

import { variants } from "@/shared/components/Button";
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

const RESTORE_OPTIONS = [
  { value: "automaticBatchRestore", label: "Batch restore" },
  { value: "automaticImmediateRestore", label: "Immediate restore" },
];

const DeviceSettingsModal = ({ onDismiss }: DeviceSettingsModalProps) => {
  // Curtail
  const [curtailSequence, setCurtailSequence] = useState("with_miners");
  const [curtailOffsetMin, setCurtailOffsetMin] = useState("0");
  const [curtailMode, setCurtailMode] = useState("fullFleet");
  const [curtailTargetKw, setCurtailTargetKw] = useState("500");

  // Restore
  const [restoreSequence, setRestoreSequence] = useState("after_miners");
  const [restoreOffsetMin, setRestoreOffsetMin] = useState("5");
  const [restoreBehavior, setRestoreBehavior] = useState("automaticBatchRestore");
  const [restoreBatchSize, setRestoreBatchSize] = useState("5");
  const [restoreIntervalSec, setRestoreIntervalSec] = useState("30");

  const showCurtailOffset = curtailSequence !== "with_miners";
  const showRestoreOffset = restoreSequence !== "with_miners";
  const showCurtailTarget = curtailMode === "fixedKwReduction";
  const showRestoreBatch = restoreBehavior === "automaticBatchRestore";

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
            {showCurtailOffset ? (
              <Input
                id="curtail-offset"
                label="Offset (minutes)"
                initValue={curtailOffsetMin}
                onChange={(v) => setCurtailOffsetMin(v)}
                type="number"
              />
            ) : <div />}
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
            {showCurtailTarget ? (
              <Input
                id="curtail-target"
                label="Target (kW)"
                initValue={curtailTargetKw}
                onChange={(v) => setCurtailTargetKw(v)}
                type="number"
              />
            ) : <div />}
          </div>
        </div>

        <div className="h-px bg-border-5" />

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
            {showRestoreOffset ? (
              <Input
                id="restore-offset"
                label="Offset (minutes)"
                initValue={restoreOffsetMin}
                onChange={(v) => setRestoreOffsetMin(v)}
                type="number"
              />
            ) : <div />}
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Select
              id="restore-behavior"
              label="Behavior"
              options={RESTORE_OPTIONS}
              value={restoreBehavior}
              onChange={setRestoreBehavior}
              forceBelow
            />
            {showRestoreBatch ? (
              <Input
                id="restore-batch-size"
                label="Batch size (devices)"
                initValue={restoreBatchSize}
                onChange={(v) => setRestoreBatchSize(v)}
                type="number"
              />
            ) : <div />}
          </div>
          {showRestoreBatch && (
            <div className="grid grid-cols-2 gap-3">
              <Input
                id="restore-interval"
                label="Batch interval (seconds)"
                initValue={restoreIntervalSec}
                onChange={(v) => setRestoreIntervalSec(v)}
                type="number"
              />
              <div />
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
};

export default DeviceSettingsModal;
