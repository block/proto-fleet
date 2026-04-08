import clsx from "clsx";

import { DEFAULT_PRESETS, isPresetVisibleForTimeframe, PresetId } from "./constants";
import { Timeframe } from "./types";

interface PresetListProps {
  activePreset?: PresetId | string;
  timeframe?: Timeframe;
  customPresets?: Array<{ label: string; startDate: Date; endDate: Date }>;
  onPresetClick: (presetId: PresetId) => void;
  onCustomPresetClick?: (preset: { label: string; startDate: Date; endDate: Date }) => void;
  testId?: string;
}

const PresetList = ({
  activePreset,
  timeframe,
  customPresets,
  onPresetClick,
  onCustomPresetClick,
  testId,
}: PresetListProps) => {
  const visiblePresets = DEFAULT_PRESETS.filter((p) => isPresetVisibleForTimeframe(p.id, timeframe));

  return (
    <div className="flex min-w-[140px] flex-col border-r border-border-5 pr-4" data-testid={testId}>
      <span className="mb-2 text-200 text-text-primary-50">Presets</span>
      <div className="flex flex-col gap-0.5">
        {visiblePresets.map((preset) => (
          <button
            key={preset.id}
            type="button"
            className={clsx(
              "cursor-pointer rounded-lg px-3 py-1.5 text-left text-300 transition-colors",
              activePreset === preset.id
                ? "bg-core-primary-5 text-text-primary"
                : "text-text-primary-70 hover:bg-core-primary-5 hover:text-text-primary",
            )}
            onClick={() => onPresetClick(preset.id)}
            data-testid={testId ? `${testId}-${preset.id}` : undefined}
          >
            {preset.label}
          </button>
        ))}
        {customPresets?.map((preset, index) => (
          <button
            key={`custom-${index}`}
            type="button"
            className={clsx(
              "cursor-pointer rounded-lg px-3 py-1.5 text-left text-300 transition-colors",
              activePreset === preset.label
                ? "bg-core-primary-5 text-text-primary"
                : "text-text-primary-70 hover:bg-core-primary-5 hover:text-text-primary",
            )}
            onClick={() => onCustomPresetClick?.(preset)}
            data-testid={testId ? `${testId}-custom-${index}` : undefined}
          >
            {preset.label}
          </button>
        ))}
      </div>
    </div>
  );
};

export default PresetList;
