import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";

interface PoolWidgetProps {
  loading: boolean;
  label: string;
  onTogglePopover: () => void;
}

const PoolWidget = ({ loading, label, onTogglePopover }: PoolWidgetProps) => {
  return (
    <WidgetWrapper
      onClick={loading ? undefined : onTogglePopover}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
      })}
    >
      {label}
    </WidgetWrapper>
  );
};

export default PoolWidget;
