import { capabilities } from "../capabilities";
import Checkbox from "@/shared/components/Checkbox";

export const CapabilityFields = ({
  value,
  allowed,
  onChange,
  required = 1n,
  options = capabilities,
}: {
  value: bigint;
  allowed: bigint;
  required?: bigint;
  options?: readonly (typeof capabilities)[number][];
  onChange: (value: bigint) => void;
}) => (
  <div className="flex flex-col gap-3">
    {options.map((capability) => (
      <label key={String(capability.value)} className="flex items-start gap-3 text-300 text-text-primary">
        <Checkbox
          ariaLabel={capability.label}
          className="shrink-0"
          checked={(value & capability.value) !== 0n}
          disabled={(required & capability.value) !== 0n || (allowed & capability.value) === 0n}
          onChange={(event) => onChange(event.target.checked ? value | capability.value : value & ~capability.value)}
        />
        <span>
          {capability.label}
          {"description" in capability ? (
            <span className="mt-1 block text-200 text-text-primary-50">{capability.description}</span>
          ) : null}
        </span>
      </label>
    ))}
  </div>
);
