import type { RunInDraft } from "./defaults";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";

const fields = [
  ["durationMinutes", "Duration (minutes)"],
  ["sampleIntervalSeconds", "Sample interval (seconds)"],
  ["cooldownMinutes", "Cooldown (minutes)"],
  ["boardPowerTargetWatts", "Power target per hashboard (W)"],
] as const;

const RunInForm = ({
  draft,
  onChange,
  disabled,
}: {
  draft: RunInDraft;
  onChange: (draft: RunInDraft) => void;
  disabled?: boolean;
}) => (
  <div className="space-y-4">
    {fields.map(([field, label]) => (
      <Input
        key={field}
        id={`run-in-${field}`}
        label={label}
        initValue={draft[field]}
        disabled={disabled}
        inputMode="numeric"
        onChange={(value) => onChange({ ...draft, [field]: value })}
      />
    ))}
    <label className="flex items-center gap-3">
      <Checkbox
        checked={draft.stopMiningOnFinish}
        disabled={disabled}
        onChange={(event) => onChange({ ...draft, stopMiningOnFinish: event.target.checked })}
      />
      Stop mining when finished
    </label>
  </div>
);

export default RunInForm;
