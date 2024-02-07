import { ReactNode } from "react";
import clsx from "clsx";

import Spinner from "components/Spinner";

import { sizes, variants } from "./constants";

interface ButtonProps {
  className?: string;
  disabled?: boolean;
  prefixIcon?: ReactNode;
  loading?: boolean;
  onClick: () => void;
  size: keyof typeof sizes;
  suffixIcon?: ReactNode;
  text?: string;
  variant: keyof typeof variants;
}

const Button = ({
  className,
  disabled,
  prefixIcon,
  loading,
  onClick,
  size,
  suffixIcon,
  text,
  variant,
}: ButtonProps) => {
  const prefix = loading ? <Spinner /> : prefixIcon;
  const gap = size === sizes.compact ? "w-2" : "w-3";

  return (
    <button
      type="button"
      className={clsx(
        "flex items-center justify-center rounded-lg",
        {
          "text-emphasis-400": size === sizes.base,
        },
        {
          "text-emphasis-300": size === sizes.compact,
        },
        {
          "px-4 py-3": size === sizes.base && text,
        },
        {
          "p-3": size === sizes.base && !text,
        },
        {
          "px-3 py-1": size === sizes.compact && text,
        },
        {
          "p-[10px]": size === sizes.compact && !text,
        },
        {
          "text-black-100 bg-black-100/5 hover:bg-black-100/20":
            variant === variants.secondary && !disabled,
        },
        {
          "text-black-100/50 bg-black-100/5":
            variant === variants.secondary && disabled,
        },
        {
          "text-white-100 bg-warning-90 hover:bg-warning-90/80":
            variant === variants.accent && !disabled,
        },
        {
          "text-white-100 bg-warning-90/40":
            variant === variants.accent && disabled,
        },
        className
      )}
      disabled={disabled}
      onClick={onClick}
    >
      {prefix}
      {text && prefix && <div className={gap} />}
      {text}
      {text && suffixIcon && <div className={gap} />}
      {suffixIcon}
    </button>
  );
};

export default Button;
