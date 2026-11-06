import { useMemo } from "react";
import StatusModalIcon from "./StatusModalIcon";
import StatusModalLayout, { type StatusModalLayoutError } from "./StatusModalLayout";
import { type MinerStatusModalProps } from "./types";
import { formatReportedTimestamp } from "./utils";
import { Alert, ControlBoard, Fan, Hashboard, Info, LightningAlt, Success } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";

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
  onAssignPoolConfiguration,
  onAuthenticateMiner,
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
    const flatErrors: StatusModalLayoutError[] = [];
    if (needsAuthentication) {
      flatErrors.push({
        key: "authentication-needed",
        icon: <Alert />,
        title: "Authentication needed",
        subtitle: "Enter miner credentials so Fleet can connect to this miner.",
        action: onAuthenticateMiner ? (
          <Button size={sizes.compact} variant={variants.secondary} onClick={onAuthenticateMiner}>
            Authenticate miner
          </Button>
        ) : undefined,
      });
    }
    if (needsMiningPool) {
      flatErrors.push({
        key: "pool-required",
        icon: <Alert />,
        title: "Pool required",
        subtitle: "This miner needs a pool configuration to start mining.",
        action: onAssignPoolConfiguration ? (
          <Button
            size={sizes.compact}
            variant={variants.secondary}
            disabled={needsAuthentication}
            onClick={onAssignPoolConfiguration}
          >
            Assign pool configuration
          </Button>
        ) : undefined,
      });
    }
    Object.entries(errors || {}).forEach(([componentType, componentErrors]) => {
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
  }, [errors, needsAuthentication, needsMiningPool, onAssignPoolConfiguration, onAuthenticateMiner]);

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
