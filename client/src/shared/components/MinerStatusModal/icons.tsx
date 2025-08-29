import {
  ControlBoard,
  Fan,
  Hashboard,
  LightningAlt,
} from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

export const R2_ICONS = {
  fan: <Fan width={iconSizes.medium} />,
  hashboard: <Hashboard width={iconSizes.medium} />,
  controlBoard: <ControlBoard width={iconSizes.medium} />,
  psu: <LightningAlt width={iconSizes.medium} />,
} as const;
