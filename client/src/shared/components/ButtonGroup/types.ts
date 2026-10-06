import { ReactNode } from "react";

import { type ButtonVariant } from "@/shared/components/Button";

export interface ButtonProps {
  ariaExpanded?: boolean;
  ariaHasPopup?: boolean | "menu" | "dialog" | "listbox" | "tree" | "grid";
  ariaLabel?: string;
  className?: string;
  disabled?: boolean;
  loading?: boolean;
  onClick?: () => void;
  variant: ButtonVariant;
  suffixIcon?: ReactNode;
  prefixIcon?: ReactNode;
  testId?: string;
  text?: string;
  textColor?: string;
  borderColor?: string;
}
