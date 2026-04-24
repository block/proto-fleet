import { MouseEvent, ReactNode } from "react";
import clsx from "clsx";

import { sizes, variants } from "./constants";
import ProgressCircular from "@/shared/components/ProgressCircular";

export type ButtonVariant = keyof typeof variants;

export interface ButtonProps {
  ariaLabel?: string;
  ariaHasPopup?: boolean | "menu" | "dialog" | "listbox" | "tree" | "grid";
  ariaExpanded?: boolean;
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
  textOnlyUnderlineOnHover?: boolean;
  variant: ButtonVariant;
}

const Button = ({
  ariaLabel,
  ariaHasPopup,
  ariaExpanded,
  borderColor = "border-core-accent-fill",
  className,
  children,
  disabled,
  loading,
  onClick,
  prefixIcon,
  size: sizeProp,
  suffixIcon,
  testId,
  text,
  textColor = "text-text-emphasis",
  textOnlyUnderlineOnHover = true,
  variant,
}: ButtonProps) => {
  const size = sizeProp ?? (variant === "textOnly" ? "textOnly" : "base");
  const primary = variant === variants.primary;
  const accent = variant === variants.accent;
  const secondary = variant === variants.secondary;
  const danger = variant === variants.danger;
  const ghost = variant === variants.ghost;
  const secondaryDanger = variant === variants.secondaryDanger;
  const textOnly = variant === variants.textOnly;
  const base = size === sizes.base;
  const compact = size === sizes.compact;
  const gap = compact ? "w-2" : "w-3";
  const prefix = loading ? <ProgressCircular size={12} indeterminate /> : prefixIcon;
  const disabledState = disabled || loading;

  return (
    <button
      type="button"
      aria-label={ariaLabel}
      aria-haspopup={ariaHasPopup}
      aria-expanded={ariaExpanded}
      className={clsx(
        "group flex h-fit items-center justify-center rounded-3xl whitespace-nowrap outline-none focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base",
        {
          "cursor-pointer": !disabledState,
          "cursor-not-allowed": disabledState,
        },
        // font size
        {
          "text-emphasis-300": base || compact || textOnly,
        },
        // padding
        {
          "px-3 py-2": base && (text || children),
          "px-2.5 py-2.5": base && !text && !children,
          "px-3 py-1": compact && (text || children),
          "px-2 py-2": compact && !text && !children,
        },
        // color and bg - primary
        {
          "bg-core-primary-fill text-text-contrast hover:opacity-80": primary && !disabledState,
          "bg-core-primary-fill text-text-contrast opacity-40": primary && disabledState,
        },
        // color and bg - accent
        {
          "bg-core-accent-fill text-text-base-contrast-static hover:opacity-80": accent && !disabledState,
          "bg-core-accent-fill text-text-base-contrast-static opacity-40": accent && disabledState,
        },
        // color and bg - secondary
        {
          "bg-core-primary-5 text-text-primary hover:opacity-80": secondary && !disabledState,
          "bg-core-primary-5 text-text-primary-50": secondary && disabledState,
        },
        // color and bg - danger
        {
          "bg-intent-critical-fill text-text-base-contrast-static hover:bg-intent-critical-text hover:opacity-80":
            danger && !disabledState,
          "bg-intent-critical-fill text-text-base-contrast-static opacity-40": danger && disabledState,
        },
        // color and bg - ghost
        {
          "shadow-50": ghost,
          "bg-surface-default text-text-primary hover:bg-core-primary-5 hover:opacity-80 hover:shadow-none":
            ghost && !disabledState,
          "bg-surface-default text-text-primary-50": ghost && disabledState,
        },
        // color and bg - secondary danger
        {
          "bg-intent-critical-10 text-text-critical hover:bg-intent-critical-20 hover:opacity-80":
            secondaryDanger && !disabledState,
          "bg-intent-critical-10 text-intent-critical-80": secondaryDanger && disabledState,
        },
        // color and bg - text only
        {
          [textColor]: textOnly && !disabledState,
          [`${textColor}/40`]: textOnly && disabledState,
          "hover:opacity-70": textOnly && !textOnlyUnderlineOnHover && !disabledState,
        },
        className,
      )}
      disabled={disabledState}
      onClick={onClick}
      data-testid={testId}
    >
      {prefix}
      {(text || children) && prefix ? <div className={gap} /> : null}
      <div className="flex flex-col">
        <div className={clsx({ "mb-[2px] group-hover:mb-0": textOnly && textOnlyUnderlineOnHover })}>
          {text}
          {children}
        </div>
        {textOnly && !disabledState && textOnlyUnderlineOnHover ? (
          <div className={clsx("-mt-[2px] w-full opacity-20 group-hover:border-b-2", borderColor)} />
        ) : null}
      </div>
      {(text || children) && suffixIcon ? <div className={gap} /> : null}
      {suffixIcon}
    </button>
  );
};

export default Button;
