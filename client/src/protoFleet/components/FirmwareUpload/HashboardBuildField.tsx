import Select from "@/shared/components/Select";

export default function HashboardBuildField({
  value,
  onChange,
  disabled = false,
}: {
  value: string;
  onChange: (value: string) => void;
  disabled?: boolean;
}) {
  return (
    <Select
      id="hashboard-firmware-build"
      label="Hashboard build"
      value={value}
      onChange={onChange}
      disabled={disabled}
      forceBelow
      options={[
        { value: "prod", label: "Production (prod)" },
        { value: "dev", label: "Development (dev)" },
        { value: "mfgtest", label: "Manufacturing test (mfgtest)" },
      ]}
    />
  );
}
