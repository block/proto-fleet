import { ReactNode } from "react";

import { type variants } from "components/Button";

export interface ButtonProps {
  className?: string;
  loading?: boolean;
  onClick?: () => void;
  variant: keyof typeof variants;
  suffixIcon?: ReactNode;
  testId?: string;
  text?: string;
}
