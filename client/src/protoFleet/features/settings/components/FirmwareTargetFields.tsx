import Input from "@/shared/components/Input";
import Select, { type SelectOption } from "@/shared/components/Select";

interface FirmwareTargetFieldsProps {
  /** Field-id prefix: `${idPrefix}-target-manufacturer`, `${idPrefix}-target-model`, `${idPrefix}-version`. */
  idPrefix: string;
  manufacturerOptions: SelectOption[];
  modelOptions: SelectOption[];
  manufacturer: string;
  model: string;
  version: string;
  disabled: boolean;
  onManufacturerChange: (value: string) => void;
  onModelChange: (value: string) => void;
  onVersionChange: (value: string) => void;
}

/**
 * The Manufacturer/Model selects and firmware version input shared by the
 * firmware upload and edit-metadata dialogs. Selecting a manufacturer clears
 * the model.
 */
const FirmwareTargetFields = ({
  idPrefix,
  manufacturerOptions,
  modelOptions,
  manufacturer,
  model,
  version,
  disabled,
  onManufacturerChange,
  onModelChange,
  onVersionChange,
}: FirmwareTargetFieldsProps) => (
  <>
    <div className="grid gap-4 tablet:grid-cols-2">
      <Select
        id={`${idPrefix}-target-manufacturer`}
        label="Manufacturer"
        options={manufacturerOptions}
        value={manufacturer}
        onChange={(value) => {
          onManufacturerChange(value);
          onModelChange("");
        }}
        disabled={disabled}
        forceBelow
      />
      <Select
        id={`${idPrefix}-target-model`}
        label="Model"
        options={modelOptions}
        value={model}
        onChange={onModelChange}
        disabled={disabled || !manufacturer}
        forceBelow
      />
    </div>
    <Input
      id={`${idPrefix}-version`}
      label="Firmware version"
      initValue={version}
      onChange={onVersionChange}
      disabled={disabled}
      required
    />
  </>
);

export default FirmwareTargetFields;
