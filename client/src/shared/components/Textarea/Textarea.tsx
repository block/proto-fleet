import { ChangeEvent, Fragment, KeyboardEvent, RefObject, useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import { DismissCircle } from "@/shared/assets/icons";
import Tooltip from "@/shared/components/Tooltip";
import { positions } from "@/shared/constants";

interface TextareaProps {
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
  inputRef?: RefObject<HTMLTextAreaElement>;
  keyboardShortcuts?: string[];
  label: string;
  maxLength?: number;
  onChange?: (value: string, id: string) => void;
  onKeyDown?: (key: string) => void;
  testId?: string;
  tooltip?: { header: string; body: string };
  onFocus?: () => void;
  onBlur?: () => void;
  rows?: number;
  required?: boolean;
}

const length = (value: string | number) => {
  if (typeof value === "string") {
    return value.length;
  }
  return String(value).length;
};

const Textarea = ({
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
  onFocus,
  onBlur,
  rows = 5,
  required,
}: TextareaProps) => {
  const [value, setValue] = useState(initValue);
  const [prevInitValue, setPrevInitValue] = useState(initValue);
  if (initValue !== prevInitValue) {
    setPrevInitValue(initValue);
    setValue(initValue);
  }

  // keep the error state until the animation is finished
  const [validationError, setValidationError] = useState(error);
  const [prevError, setPrevError] = useState(error);
  if (error && error !== prevError) {
    setPrevError(error);
    setValidationError(error);
  }

  const [focused, setFocused] = useState(false);
  const fallbackRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (error) return;
    const timeoutId = setTimeout(() => {
      setValidationError(error);
      setPrevError(error);
    }, 200);
    return () => clearTimeout(timeoutId);
  }, [error]);

  const handleChange = useCallback(
    (event?: ChangeEvent<HTMLTextAreaElement>) => {
      const newValue = event?.target.value || "";
      setValue(newValue);
      onChange?.(newValue, id);
    },
    [onChange, id],
  );

  const handleKeyDown = useCallback(
    (e: KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        e.currentTarget.blur();
      }
      onKeyDown?.(e.key);
    },
    [onKeyDown],
  );

  const handleClear = useCallback(() => {
    setValue("");
    onChange?.("", id);
  }, [id, onChange]);

  return (
    <div className="relative">
      <div className="relative">
        <textarea
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
              "focus:border-border-20 focus:ring-4 focus:ring-core-primary-5": !error && !compact && !disabled,
            },
            {
              "border border-intent-critical-50 focus:ring-4 focus:ring-intent-critical-20": error,
            },
            { "pt-[28px]": !hideLabelOnFocus },
            { "pl-4": !compact },
            className,
          )}
          rows={rows}
          onChange={handleChange}
          onKeyDown={handleKeyDown}
          maxLength={maxLength}
          autoComplete="off"
          value={value}
          ref={inputRef || fallbackRef}
          disabled={disabled}
          autoFocus={autoFocus}
          required={required}
          aria-required={required || undefined}
          aria-invalid={!!error || undefined}
          aria-describedby={typeof error === "string" && error ? `${id}-error` : undefined}
          onFocus={() => {
            onFocus?.();
            setFocused(true);
          }}
          onBlur={() => {
            onBlur?.();
            setFocused(false);
          }}
        />
        <label
          htmlFor={id}
          className={clsx(
            "absolute text-text-primary-50",
            { "cursor-text": !disabled },
            { "text-300": !(length(value) || focused) },
            { "left-0": compact },
            { "left-[17px]": !compact },
            {
              "top-7 -translate-y-1/2": !(length(value) || focused) && !compact,
            },
            { "top-0": !(length(value) || focused) && compact },
            { "top-[7px] text-200": length(value) || focused },
            {
              "transition-[top] ease-in-out peer-focus:top-[7px] peer-focus:text-200": !hideLabelOnFocus,
            },
            { "peer-focus:invisible": hideLabelOnFocus },
            { invisible: hideLabelOnFocus && (length(value) || focused) },
          )}
        >
          {label}
        </label>
        {tooltip ? (
          <div className="absolute top-7 right-4 -translate-y-1/2 transform">
            <Tooltip header={tooltip.header} body={tooltip.body} position={positions["top left"]} />
          </div>
        ) : null}
        {dismiss && length(value) && !compact ? (
          <div
            className={clsx("absolute right-4", {
              "top-1": compact,
              "top-7 -translate-y-1/2 transform": !compact,
            })}
          >
            <DismissCircle ariaLabel={`Clear ${label}`} onClick={handleClear} className="text-text-primary-70" />
          </div>
        ) : null}
        {keyboardShortcuts && !length(value) ? (
          <div className="absolute top-7 right-4 flex -translate-y-1/2 transform space-x-[2px] rounded-sm bg-core-primary-5 px-2 text-300 font-semibold text-text-primary-30 shadow-100">
            {keyboardShortcuts.map((shortcut, index) => (
              <Fragment key={index}>{shortcut}</Fragment>
            ))}
          </div>
        ) : null}
      </div>
      <div
        className={clsx(
          "text-200 text-intent-critical-fill",
          "transition-[opacity,max-height,margin-top] duration-200 ease-in-out",
          { "max-h-0 opacity-0": !error || error === true },
          { "mt-2 max-h-24 opacity-100": error && error !== true },
        )}
      >
        <div className="flex items-start space-x-1">
          <div className="mt-1.5 h-1 w-[10px] shrink-0 rounded-full bg-intent-critical-20" />
          <div
            id={typeof error === "string" && error ? `${id}-error` : undefined}
            data-testid={`${testId}-validation-error`}
            className="whitespace-pre-wrap"
          >
            {validationError}
          </div>
        </div>
      </div>
    </div>
  );
};

export default Textarea;
