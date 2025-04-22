import StatusCircle from "../StatusCircle";
import StatComponent, { type StatProps } from ".";
import { HashboardIndicator } from "@/shared/assets/icons";

const icons = {
  hashboardIndicator: <HashboardIndicator activeHashboard={2} />,
  status: <StatusCircle status="warning" variant="simple" width="w-1.5" />,
};

type StatStoryType = StatProps & {
  iconType: keyof typeof icons | "none";
  hasText: boolean;
};

export const Stat = ({
  size,
  chartStatus,
  iconType,
  hasText,
}: StatStoryType) => {
  const icon = iconType === "none" ? undefined : icons[iconType];
  const text = hasText ? "2.1% below expected" : undefined;

  return (
    <div className="w-60 px-4">
      <StatComponent
        label="Hashrate"
        value={255.4}
        text={text}
        units="TH/s"
        size={size}
        icon={icon}
        headingLevel={3}
        chartPercentage={74.2}
        chartStatus={chartStatus}
      />
    </div>
  );
};

Stat.args = {
  size: "small",
  chartStatus: "warning",
  iconType: "hashboardIndicator",
  hasText: true,
};

Stat.argTypes = {
  size: {
    control: "select",
    options: ["small", "medium", "large"],
  },
  chartStatus: {
    control: "select",
    options: ["success", "warning", "critical", "neutral"],
  },
  iconType: {
    control: "select",
    options: ["none", "hashboardIndicator", "status"],
  },
  hasText: {
    control: "boolean",
  },
};

export default {
  title: "Components (Shared)/Stat",
};
