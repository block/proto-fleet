import { HASHBOARD_BUILDS, type HashboardBuild } from "@/protoFleet/api/useFirmwareApi";
import Select from "@/shared/components/Select";

const buildLabels: Record<HashboardBuild, string> = {
  prod: "Production (prod)",
  dev: "Development (dev)",
  mfgtest: "Manufacturing test (mfgtest)",
};

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
      options={HASHBOARD_BUILDS.map((build) => ({ value: build, label: buildLabels[build] }))}
    />
  );
}
