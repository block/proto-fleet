import { ControlBoard, Fan, Hashboard, Lightning } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

export const R2_ICONS = {
  fan: <Fan width={iconSizes.medium} />,
  hashboard: <Hashboard width={iconSizes.medium} />,
  controlBoard: <ControlBoard width={iconSizes.medium} />,
  psu: <Lightning width={iconSizes.medium} />,
} as const;
