import { ButtonProps } from "./types";
import { variants } from "@/shared/components/Button";

const sortPrimaryButton = (buttons: ButtonProps[], last: boolean) => {
  const firstPrimary = last ? 1 : -1;
  const secondPrimary = last ? -1 : 1;
  return buttons.sort((a, b) => {
    if (a.variant === variants.primary) {
      return firstPrimary;
    }
    if (b.variant === variants.primary) {
      return secondPrimary;
    }
    return 0;
  });
};

export const sortPrimaryButtonFirst = (buttons: ButtonProps[]) => sortPrimaryButton(buttons, false);

export const sortPrimaryButtonLast = (buttons: ButtonProps[]) => sortPrimaryButton(buttons, true);
