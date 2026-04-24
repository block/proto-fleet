import clsx from "clsx";

type SwitchProps = {
  label?: string;
  checked?: boolean;
  setChecked?: (checked: boolean | ((prev: boolean) => boolean)) => void;
  disabled?: boolean;
};

const Switch = ({ label, checked, setChecked, disabled = false }: SwitchProps) => {
  return (
    <label className="inline-flex cursor-pointer items-center gap-4 select-none">
      {label ? <span className="text-300">{label}</span> : null}
      <div className="relative inline-block">
        <input
          type="checkbox"
          className="peer absolute h-0 w-0 opacity-0"
          disabled={disabled}
          checked={checked}
          onChange={() => {
            if (setChecked) setChecked((prev: boolean) => !prev);
          }}
        />
        <span
          className={clsx(
            // Base styles
            "relative block h-5 w-8 rounded-full bg-core-primary-10",
            "transition-all duration-200 ease-out",
            "origin-center",

            "peer-hover:scale-[1.20] peer-hover:border-core-primary-10",
            "focus:bg-core-primary-50",

            // Handle circle styles using pseudo-element
            "before:absolute before:h-[18px] before:w-[18px]",
            "before:top-[1px] before:left-[1px] before:rounded-full before:bg-white",
            "before:transition-transform before:duration-200 before:ease-out",
            "before:shadow-100",
            // Keep the circle size on hover
            "peer-hover:peer-not-disabled:before:scale-[0.834]",

            "peer-checked:bg-core-accent-fill peer-checked:before:translate-x-3",
            "peer-checked:peer-disabled:bg-core-accent-50",

            // Disable hover effect when disabled
            "peer-disabled:peer-hover:scale-100",
          )}
        />
      </div>
    </label>
  );
};

export default Switch;
