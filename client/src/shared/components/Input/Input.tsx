import {
  ChangeEvent,
  Fragment,
  KeyboardEvent,
  ReactNode,
  RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import clsx from "clsx";

import useValueWidth from "./useValueWidth";
import { DismissCircle, Eye } from "@/shared/assets/icons";
import Tooltip from "@/shared/components/Tooltip";
import { positions } from "@/shared/constants";

interface InputProps {
  autoFocus?: boolean;
  compact?: boolean;
  className?: string;
  disabled?: boolean;
  dismiss?: boolean;
  // Error message is optional in error state
  error?: boolean | string;
  hideLabelOnFocus?: boolean;
  id: string;
  initValue?: string | number;
  inputRef?: RefObject<HTMLInputElement>;
  keyboardShortcuts?: string[];
  label: string;
  maxLength?: number;
  onChange?: (value: string, id: string) => void;
  onKeyDown?: (key: string) => void;
  testId?: string;
  tooltip?: { header: string; body: string };
  type?: string;
  statusIcon?: ReactNode;
  onFocus?: () => void;
  onBlur?: () => void;
  units?: string;
}

const length = (value: string | number) => {
  if (typeof value === "string") {
    return value.length;
  }
  return String(value).length;
};

const Input = ({
  autoFocus,
  compact,
  className,
  dismiss,
  disabled,
  error = false,
  hideLabelOnFocus,
  id,
  initValue = "",
  inputRef,
  keyboardShortcuts,
  label,
  maxLength,
  onChange,
  onKeyDown,
  testId,
  tooltip,
  type = "text",
  statusIcon,
  onFocus,
  onBlur,
  units,
}: InputProps) => {
  const [value, setValue] = useState(initValue);
  // keep the error state until the animation is finished
  const [validationError, setValidationError] = useState(error);
  const [timeoutId, setTimeoutId] = useState<ReturnType<typeof setTimeout>>();
  const [inputType, setInputType] = useState(type);
  const [focused, setFocused] = useState(false);
  const fallbackRef = useRef<HTMLInputElement>(null);
  const valueWidth = useValueWidth(value, inputRef || fallbackRef, units);

  useEffect(() => {
    setValue(initValue);
  }, [initValue]);

  useEffect(() => {
    setInputType(type);
  }, [type]);

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
    [onChange, id],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLInputElement>) => {
      onKeyDown?.(e.key);
    },
    [onKeyDown],
  );

  // when eye icon is clicked, display and hide the password
  const togglePasswordVisibility = useCallback(() => {
    setInputType(inputType === "password" ? "text" : "password");
  }, [inputType]);

  return (
    <div className="relative">
      <div className="relative">
        <input
          type={inputType}
          id={id}
          data-testid={testId}
          className={clsx(
            "peer w-full rounded-lg text-300 text-text-primary outline-hidden",
            "transition duration-200 ease-in-out",
            { "bg-surface-base": !disabled },
            { "bg-core-primary-5": disabled },
            {
              "border border-border-5": !error && !compact,
            },
            {
              "border border-border-20 focus:ring-4 focus:ring-surface-10":
                !error && !compact && !disabled,
            },
            {
              "border border-intent-critical-50 focus:ring-4 focus:ring-intent-critical-20":
                error,
            },
            { "pt-[18px]": !hideLabelOnFocus },
            { "h-14 pl-4": !compact },
            { "pr-4": !compact && !tooltip && type !== "password" },
            { "pr-10": !compact && tooltip && type !== "password" },
            { "pr-20": !compact && tooltip && type === "password" },
            { "h-6": compact },
            { "no-spinner": type === "number" },
            className,
          )}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          maxLength={maxLength}
          autoComplete="off"
          value={value}
          ref={inputRef || fallbackRef}
          disabled={disabled}
          autoFocus={autoFocus}
          onFocus={() => {
            onFocus && onFocus();
            setFocused(true);
          }}
          onBlur={() => {
            onBlur && onBlur();
            setFocused(false);
          }}
        />
        {units && valueWidth !== undefined && value && (
          <span
            className={clsx(
              "pointer-events-none absolute bottom-0 left-0 flex items-center text-300 text-text-primary-70",
              {
                "pt-[18px]": !hideLabelOnFocus,
                "h-14 pl-4": !compact,
                "h-6": compact,
              },
            )}
            style={{ transform: `translateX(${valueWidth + 4}px)` }}
          >
            {units}
          </span>
        )}
        <label
          htmlFor={id}
          className={clsx(
            "absolute text-text-primary-50",
            { "cursor-text": !disabled },
            { "text-300": !(length(value) || focused) },
            { "left-0": compact },
            { "left-[17px]": !compact },
            {
              "top-1/2 -translate-y-1/2":
                !(length(value) || focused) && !compact,
            },
            { "top-0": !(length(value) || focused) && compact },
            { "top-[7px] text-200": length(value) || focused },
            {
              "duration-150ms transition-[top] ease-in-out peer-focus:top-[7px] peer-focus:text-200":
                !hideLabelOnFocus,
            },
            { "peer-focus:invisible": hideLabelOnFocus },
            { invisible: hideLabelOnFocus && (length(value) || focused) },
          )}
        >
          {label}
        </label>
        {tooltip && (
          <div className="absolute top-7 right-4 -translate-y-1/2 transform">
            <Tooltip
              header={tooltip.header}
              body={tooltip.body}
              position={positions["top left"]}
            />
          </div>
        )}
        {dismiss && length(value) && !compact ? (
          <div
            className={clsx("absolute right-4", {
              "top-1": compact,
              "top-7 -translate-y-1/2 transform": !compact,
            })}
          >
            <DismissCircle
              onClick={handleChange}
              className="hover:cursor-pointer"
              opacity="0.7"
            />
          </div>
        ) : undefined}
        {keyboardShortcuts && !length(value) ? (
          <div className="absolute top-7 right-4 flex -translate-y-1/2 transform space-x-[2px] rounded-sm bg-core-primary-5 px-2 text-300 font-semibold text-text-primary-30 shadow-100">
            {keyboardShortcuts.map((shortcut, index) => (
              <Fragment key={index}>{shortcut}</Fragment>
            ))}
          </div>
        ) : undefined}
        {(type === "password" || statusIcon !== undefined) && (
          <div
            className={clsx("absolute", {
              "top-1": compact,
              "top-1/2 -translate-y-1/2 transform": !compact,
              "right-4": !tooltip,
              "right-12": tooltip,
            })}
          >
            {statusIcon ? (
              statusIcon
            ) : (
              <Eye
                onClick={togglePasswordVisibility}
                className="hover:cursor-pointer"
                testId="eye-icon"
              />
            )}
          </div>
        )}
      </div>
      <div
        className={clsx(
          "text-200 text-intent-critical-fill",
          "transition-[opacity,max-height,margin-top] duration-200 ease-in-out",
          { "max-h-0 opacity-0": !error || error === true },
          { "mt-2 max-h-10 opacity-100": error && error !== true },
        )}
      >
        <div className="flex items-center space-x-1">
          <div className="h-1 w-[10px] rounded-full bg-intent-critical-20" />
          <div data-testid={`${testId}-validation-error`}>
            {validationError}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Input;
