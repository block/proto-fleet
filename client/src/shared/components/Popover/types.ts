import { ReactNode } from "react";
import { ButtonProps, groupVariants } from "@/shared/components/ButtonGroup";
import { popoverSizes } from "@/shared/components/Popover/constants";

export type PopoverContentProps = {
  buttonGroupVariant?: keyof typeof groupVariants;
  buttons?: ButtonProps[];
  children?: ReactNode;
  className?: string;
  size?: keyof typeof popoverSizes;
  subtitle?: string;
  testId?: string;
  title?: string;
  titleSize?: string;
  closePopover?: () => void;
  closeIgnoreSelectors?: string[];
};
