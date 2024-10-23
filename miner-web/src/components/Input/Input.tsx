import {
  ChangeEvent,
  Fragment,
  KeyboardEvent,
  RefObject,
  useCallback,
  useEffect,
  useState,
} from "react";
import clsx from "clsx";

import { positions } from "common/constants";

import Tooltip from "components/Tooltip";
import { DismissCircle, Eye } from "icons";

interface InputProps {
  compact?: boolean;
  className?: string;
  dismiss?: boolean;
  error?: string;
  hideLabelOnFocus?: boolean;
  id: string;
  initValue?: string;
  inputRef?: RefObject<HTMLInputElement>;
  keyboardShortcuts?: string[];
  label: string;
  maxLength?: number;
  onChange?: (value: string, id: string) => void;
  onKeyDown?: (key: string) => void;
  readonly?: boolean;
  testId?: string;
  tooltip?: { header: string; body: string };
  type?: string;
}

const Input = ({
  compact,
  className,
  dismiss,
  error,
  hideLabelOnFocus,
  id,
  initValue = "",
  inputRef,
  keyboardShortcuts,
  label,
  maxLength,
  onChange,
  onKeyDown,
  readonly,
  testId,
  tooltip,
  type = "text",
}: InputProps) => {
  const [value, setValue] = useState(initValue);
  // keep the error state until the animation is finished
  const [validationError, setValidationError] = useState(error);
  const [timeoutId, setTimeoutId] = useState<ReturnType<typeof setTimeout>>();
  const [inputType, setInputType] = useState(type);

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
    (event?: ChangeEvent<HTMLInputElement>) => {
      const newValue = (event?.target as HTMLInputElement).value || "";
      setValue(newValue);
      onChange?.(newValue, id);
    },
    [onChange, id]
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      onKeyDown?.(e.key);
    },
    [onKeyDown]
  );

  // when eye icon is clicked, display and hide the password
  const togglePasswordVisibility = useCallback(() => {
    setInputType(inputType === "password" ? "text" : "password");
  }, [inputType]);

  return (
    <div className="relative">
      <input
        type={inputType}
        id={id}
        data-testid={testId}
        className={clsx(
          "peer rounded-lg w-full outline-none text-300 text-text-primary bg-surface-elevated-base",
          "transition-[border-color] ease-in-out duration-200",
          {
            "border border-border-5": !error && !compact,
          },
          {
            "focus:border-[1.5px] focus:border-border-primary":
              !error && !compact && !readonly,
          },
          { "border-[1.5px] border-intent-critical-50 ": error },
          { "pt-[18px]": !hideLabelOnFocus },
          { "h-14 pl-4": !compact },
          { "pr-4": !compact && !tooltip && type !== "password" },
          { "pr-10": !compact && tooltip && type !== "password" },
          { "pr-20": !compact && tooltip && type === "password" },
          { "h-6": compact },
          className
        )}
        onChange={handleChange}
        onKeyDown={handleKeyDown}
        maxLength={maxLength}
        autoComplete="off"
        value={value}
        ref={inputRef}
        readOnly={readonly}
      />
      <label
        htmlFor={id}
        className={clsx(
          "text-text-primary-50 absolute cursor-text",
          { "text-300": !value.length },
          { "left-0": compact },
          { "left-[17px]": !compact },
          { "top-[18px]": !value.length && !compact },
          { "top-[7px] text-200": value.length },
          {
            "transition-[top] ease-in-out duration-150ms peer-focus:top-[7px] peer-focus:text-200":
              !hideLabelOnFocus,
          },
          { "peer-focus:invisible": hideLabelOnFocus },
          { invisible: hideLabelOnFocus && value.length }
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
      {dismiss && value.length && !compact ? (
        <div
          className={clsx("absolute right-4", {
            "top-1": compact,
            "top-7 transform -translate-y-1/2": !compact,
          })}
        >
          <DismissCircle
            onClick={handleChange}
            className="hover:cursor-pointer"
            opacity="0.7"
          />
        </div>
      ) : undefined}
      {keyboardShortcuts && !value.length ? (
        <div className="absolute right-4 top-7 transform -translate-y-1/2 flex space-x-[2px] text-300 font-semibold text-text-primary-30 bg-core-primary-5 rounded px-2 shadow-100">
          {keyboardShortcuts.map((shortcut, index) => (
            <Fragment key={index}>{shortcut}</Fragment>
          ))}
        </div>
      ) : undefined}
      {type === "password" && (
        <div
          className={clsx("absolute", {
            "top-1": compact,
            "top-7 transform -translate-y-1/2": !compact,
            "right-4": !tooltip,
            "right-12": tooltip,
          })}
        >
          <Eye
            onClick={togglePasswordVisibility}
            className="hover:cursor-pointer"
            testId="eye-icon"
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
          <div className="w-[10px] h-1 rounded-full bg-intent-critical-20" />
          <div data-testid={`${testId}-validation-error`}>
            {validationError}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Input;
