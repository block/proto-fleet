import { variants } from "components/Button";

export interface ButtonProps {
  className?: string;
  loading?: boolean;
  onClick?: () => void;
  variant: keyof typeof variants;
  testId?: string;
  text?: string;
}
