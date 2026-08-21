import Select from "@/shared/components/Select";

export interface PickerOption {
  id: string;
  label: string;
}

interface SinglePickerFieldProps {
  id: string;
  label: string;
  options: PickerOption[];
  value: string | null;
  placeholder?: string;
  emptyMessage?: string;
  onChange: (value: string) => void;
}

const SinglePickerField = ({
  id,
  label,
  options,
  value,
  placeholder = "Pick one",
  emptyMessage = "No options",
  onChange,
}: SinglePickerFieldProps) => (
  <Select
    id={id}
    label={label}
    options={options.map((option) => ({ value: option.id, label: option.label }))}
    value={value ?? ""}
    placeholder={placeholder}
    emptyMessage={emptyMessage}
    onChange={onChange}
  />
);

export default SinglePickerField;
