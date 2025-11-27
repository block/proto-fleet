import { useMemo } from "react";
import StatusModalLayout, { type StatusModalLayoutError } from "./StatusModalLayout";
import { type MinerStatusModalProps } from "./types";
import { Alert, Checkmark, ControlBoard, Fan, Hashboard, Info, LightningAlt } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";

const componentIcons = {
  fan: <Fan width={iconSizes.medium} className="text-text-primary-70" />,
  hashboard: <Hashboard width={iconSizes.medium} className="text-text-primary-70" />,
  controlBoard: <ControlBoard width={iconSizes.medium} className="text-text-primary-70" />,
  psu: <LightningAlt width={iconSizes.medium} className="text-text-primary-70" />,
};

const MinerStatusModalContent = ({ title, subtitle, errors, isSleeping }: MinerStatusModalProps) => {
  const haserrors = Object.values(errors || {}).some((errorList) => errorList.length > 0);

  const icon = useMemo(() => {
    if (isSleeping) {
      return <Info className="text-core-primary-20" width={iconSizes.xLarge} />;
    } else if (haserrors) {
      return <Alert className="text-text-critical" width={iconSizes.xLarge} />;
    } else
      return <Checkmark className="rounded-full bg-intent-success-fill text-surface-base" width={iconSizes.xLarge} />;
  }, [haserrors, isSleeping]);

  // Transform grouped errors into flat array for layout
  const layoutErrors: StatusModalLayoutError[] = useMemo(() => {
    if (!errors) return [];

    const flatErrors: StatusModalLayoutError[] = [];
    Object.entries(errors).forEach(([componentType, componentErrors]) => {
      componentErrors.forEach((error, idx) => {
        // For MinerStatus, show componentName as title and message as subtitle
        const getErrorSubtitle = () => {
          const hasMessage = Boolean(error.message);
          const hasTimestamp = Boolean(error.timestamp);

          if (hasMessage && hasTimestamp) {
            return `${error.message} on ${formatTimestamp(error.timestamp)}`;
          }
          if (hasMessage) {
            return error.message;
          }
          if (hasTimestamp) {
            return formatTimestamp(error.timestamp);
          }
          return undefined;
        };

        const subtitle = getErrorSubtitle();

        flatErrors.push({
          key: `${componentType}_${idx}_${error.timestamp || idx}`,
          icon: componentIcons[componentType as keyof typeof componentIcons],
          title: error.componentName,
          subtitle,
          onClick: error.onClick,
        });
      });
    });
    return flatErrors;
  }, [errors]);

  return <StatusModalLayout icon={icon} title={title} subtitle={subtitle} errors={layoutErrors} />;
};

export default MinerStatusModalContent;
