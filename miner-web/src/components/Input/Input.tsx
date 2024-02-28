import { ChangeEvent, KeyboardEvent, useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import Tooltip, { positions } from "components/Tooltip";

interface InputProps {
  error?: string;
  id: string;
  initValue?: string;
  label: string;
  maxLength?: number;
  onChange?: (value: string, id: string) => void;
  onKeyDown?: (key: string) => void;
  tooltip?: { header: string; body: string };
  type?: string;
}

const Input = ({
  error,
  id,
  initValue = "",
  label,
  maxLength,
  onChange,
  onKeyDown,
  tooltip,
  type = "text",
}: InputProps) => {
  const [value, setValue] = useState(initValue);
  // keep the error state until the animation is finished
  const [validationError, setValidationError] = useState(error);
  const [timeoutId, setTimeoutId] = useState<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    setValue(initValue);
  }, [initValue]);

  useEffect(() => {
    if (error) {
      clearTimeout(timeoutId);
      setValidationError(error);
    } else if (!timeoutId) {
      // clear the error after the animation
      const newTimeoutId = setTimeout(() => {
        setValidationError(error);
      }, 200);
      setTimeoutId(newTimeoutId);
    }
  }, [error, timeoutId]);

  const handleChange = useCallback(
    (event: ChangeEvent<HTMLInputElement>) => {
      const newValue = (event.target as HTMLInputElement).value;
      setValue(newValue);
      onChange?.(newValue, id);
    },
    [onChange, id]
  );

  const handleKeyDown = useCallback((e: KeyboardEvent<HTMLInputElement>) => {
    onKeyDown?.(e.key);
  }, [onKeyDown]);

  return (
    <div className="relative">
      <input
        type={type}
        id={id}
        className={clsx(
          "peer rounded-lg w-full h-14 outline-none pt-[18px] px-4 text-300",
          "transition-[border-color] ease-in-out duration-200",
          {
            "border focus:border-[1.5px] border-border-primary/5 focus:border-border-primary":
              !error,
          },
          { "border-[1.5px] border-intent-critical-fill/50 ": error }
        )}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        maxLength={maxLength}
        autoComplete="off"
        value={value}
      />
      <label
        htmlFor={id}
        className={clsx(
          "text-text-primary/50 absolute left-[17px] transition-[top] ease-in-out duration-150ms",
          { "top-[18px] text-300": !value.length },
          { "top-[7px] text-200": value.length },
          "peer-focus:top-[7px] peer-focus:text-200"
        )}
      >
        {label}
      </label>
      {tooltip && (
        <div className="absolute right-4 top-7 transform -translate-y-1/2">
          <Tooltip
            header={tooltip.header}
            body={tooltip.body}
            position={positions["top left"]}
          />
        </div>
      )}
      <div
        className={clsx(
          "text-intent-critical-fill text-200",
          "transition-[opacity,max-height,margin-top] ease-in-out duration-200",
          { "opacity-0 max-h-0": !error },
          { "opacity-100 max-h-10 mt-2": error }
        )}
      >
        <div className="flex items-center space-x-1">
          <svg width="6" height="6" viewBox="0 0 6 6">
            <circle cx="3" cy="3" r="3" fill="currentColor" fillOpacity="0.2" />
          </svg>
          <div>{validationError}</div>
        </div>
      </div>
    </div>
  );
};

export default Input;
