import { capabilities } from "../capabilities";
import Checkbox from "@/shared/components/Checkbox";

export const CapabilityFields = ({
  value,
  allowed,
  onChange,
  required = 1n,
}: {
  value: bigint;
  allowed: bigint;
  required?: bigint;
  onChange: (value: bigint) => void;
}) => (
  <div className="flex flex-col gap-3">
    {capabilities.map((capability) => (
      <label key={String(capability.value)} className="flex items-center gap-3 text-300 text-text-primary">
        <Checkbox
          checked={(value & capability.value) !== 0n}
          disabled={(required & capability.value) !== 0n || (allowed & capability.value) === 0n}
          onChange={(event) => onChange(event.target.checked ? value | capability.value : value & ~capability.value)}
        />
        {capability.label}
      </label>
    ))}
  </div>
);
