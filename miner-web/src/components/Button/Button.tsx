import { MouseEvent, ReactNode } from "react";
import clsx from "clsx";

import Spinner from "components/Spinner";

import { sizes, variants } from "./constants";

interface ButtonProps {
  borderColor?: string;
  className?: string;
  children?: ReactNode;
  disabled?: boolean;
  loading?: boolean;
  onClick?: (e: MouseEvent<HTMLButtonElement>) => void;
  prefixIcon?: ReactNode;
  size?: keyof typeof sizes;
  suffixIcon?: ReactNode;
  testId?: string;
  text?: string;
  textColor?: string;
  variant: keyof typeof variants;
}

const Button = ({
  borderColor = "border-core-accent-fill",
  className,
  children,
  disabled,
  loading,
  onClick,
  prefixIcon,
  size,
  suffixIcon,
  testId,
  text,
  textColor = "text-text-emphasis",
  variant,
}: ButtonProps) => {
  const primary = variant === variants.primary;
  const accent = variant === variants.accent;
  const secondary = variant === variants.secondary;
  const danger = variant === variants.danger;
  const secondaryDanger = variant === variants.secondaryDanger;
  const textOnly = variant === variants.textOnly;
  const base = size === sizes.base;
  const compact = size === sizes.compact;
  const gap = compact ? "w-2" : "w-3";
  const prefix = loading ? <Spinner /> : prefixIcon;
  const disabledState = disabled || loading;

  return (
    <button
      type="button"
      className={clsx(
        "group flex items-center justify-center rounded-lg h-fit outline-0 whitespace-nowrap",
        {
          "cursor-not-allowed": disabledState,
        },
        // font size
        {
          "text-emphasis-400": base,
          "text-emphasis-300": compact,
          "text-emphasis-300 font-extrabold": textOnly,
        },
        // padding
        {
          "px-3 py-2": base && text,
          "p-2": base && !text,
          "px-2 py-[2px]": compact && text,
          "p-[6px]": compact && !text,
        },
        // color and bg - primary
        {
          "text-text-contrast bg-core-primary-fill hover:opacity-80":
            primary && !disabledState,
          "text-text-contrast bg-core-primary-fill opacity-40":
            primary && disabledState,
        },
        // color and bg - accent
        {
          "text-text-contrast bg-core-accent-fill hover:opacity-80":
            accent && !disabledState,
          "text-text-contrast bg-core-accent-fill opacity-40": accent && disabledState,
        },
        // color and bg - secondary
        {
          "text-text-primary bg-core-primary-10 hover:opacity-80":
            secondary && !disabledState,
          "text-text-primary-50 bg-core-primary-5": secondary && disabledState,
        },
        // color and bg - danger
        {
          "text-text-contrast bg-intent-critical-fill hover:bg-intent-critical-text hover:opacity-80":
            danger && !disabledState,
          "text-text-contrast bg-intent-critical-fill opacity-40":
            danger && disabledState,
        },
        // color and bg - secondary danger
        {
          "text-text-critical bg-intent-critical-10 hover:bg-intent-critical-20 hover:opacity-80":
            secondaryDanger && !disabledState,
          "text-intent-critical-80 bg-intent-critical-10":
            secondaryDanger && disabledState,
        },
        // color and bg - text only
        {
          [textColor]: textOnly && !disabledState,
          [`${textColor}/40`]: textOnly && disabledState,
        },
        className
      )}
      disabled={disabledState}
      onClick={onClick}
      data-testid={testId}
    >
      {prefix}
      {(text || children) && prefix && <div className={gap} />}
      <div className="flex flex-col">
        <div className={clsx({ "mb-[2px] group-hover:mb-0": textOnly })}>
          {text}
          {children}
        </div>
        {textOnly && !disabledState && (
          <div
            className={clsx(
              "group-hover:border-b-2 w-full opacity-20 -mt-[2px]",
              borderColor
            )}
          />
        )}
      </div>
      {(text || children) && suffixIcon && <div className={gap} />}
      {suffixIcon}
    </button>
  );
};

export default Button;
