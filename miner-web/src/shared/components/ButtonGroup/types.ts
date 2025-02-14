import { ReactNode } from "react";

import { type variants } from "@/shared/components/Button";

export interface ButtonProps {
  className?: string;
  disabled?: boolean;
  loading?: boolean;
  onClick?: () => void;
  variant: keyof typeof variants;
  suffixIcon?: ReactNode;
  testId?: string;
  text?: string;
}
