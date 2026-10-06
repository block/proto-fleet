import { useMemo } from "react";
import StatusModalIcon from "./StatusModalIcon";
import StatusModalLayout, { type StatusModalLayoutError } from "./StatusModalLayout";
import { type MinerStatusModalProps } from "./types";
import { formatReportedTimestamp } from "./utils";
import { Alert, ControlBoard, Fan, Hashboard, Info, LightningAlt, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";

const componentIcons = {
  fan: <Fan />,
  hashboard: <Hashboard />,
  controlBoard: <ControlBoard />,
  psu: <LightningAlt />,
  other: <Alert />,
};

const MINER_ASLEEP_TITLE = "Miner is asleep";
const MINER_ASLEEP_SUBTITLE = "Wake your miner to start hashing again";

const MinerStatusModalContent = ({
  title,
  subtitle,
  errors,
  isSleeping,
  isOffline,
  needsAuthentication,
  needsMiningPool,
}: MinerStatusModalProps) => {
  const haserrors = Object.values(errors || {}).some((errorList) => errorList.length > 0);

  const icon = useMemo(() => {
    if (isSleeping || isOffline) {
      return (
        <StatusModalIcon>
          <Info width={iconSizes.xLarge} />
        </StatusModalIcon>
      );
    } else if (needsAuthentication || needsMiningPool || haserrors) {
      return (
        <StatusModalIcon intent="critical">
          <Alert width={iconSizes.xLarge} />
        </StatusModalIcon>
      );
    } else {
      return (
        <StatusModalIcon intent="success">
          <Success width={iconSizes.xLarge} />
        </StatusModalIcon>
      );
    }
  }, [haserrors, isSleeping, isOffline, needsAuthentication, needsMiningPool]);

  // Determine what titles to show
  const displayTitle = isSleeping ? MINER_ASLEEP_TITLE : title;
  const displaySubtitle = isSleeping ? MINER_ASLEEP_SUBTITLE : subtitle;

  // If sleeping and has errors, show the error summary title as secondary
  // If sleeping and no errors, don't show secondary title (suppress "All systems operational")
  const secondaryTitle = isSleeping && haserrors ? title : undefined;
  const secondarySubtitle = isSleeping && haserrors ? subtitle : undefined;

  // Transform grouped errors into flat array for layout
  const layoutErrors: StatusModalLayoutError[] = useMemo(() => {
    if (!errors) return [];

    const flatErrors: StatusModalLayoutError[] = [];
    Object.entries(errors).forEach(([componentType, componentErrors]) => {
      componentErrors.forEach((error, idx) => {
        flatErrors.push({
          key: `${componentType}_${idx}_${error.timestamp || idx}`,
          icon: componentIcons[componentType as keyof typeof componentIcons],
          title: error.message,
          subtitle: [error.componentName, formatReportedTimestamp(error.timestamp)].filter(Boolean).join(" · "),
          onClick: error.onClick,
        });
      });
    });
    return flatErrors;
  }, [errors]);

  return (
    <StatusModalLayout
      icon={icon}
      title={displayTitle}
      subtitle={displaySubtitle}
      secondaryTitle={secondaryTitle}
      secondarySubtitle={secondarySubtitle}
      errors={layoutErrors}
    />
  );
};

export default MinerStatusModalContent;
