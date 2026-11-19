import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";

interface PoolWidgetProps {
  loading: boolean;
  label: string;
  isOpen: boolean;
  onTogglePopover: () => void;
}

const PoolWidget = ({ loading, label, isOpen, onTogglePopover }: PoolWidgetProps) => {
  return (
    <WidgetWrapper
      onClick={loading ? undefined : onTogglePopover}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
      })}
      isOpen={isOpen}
    >
      {label}
    </WidgetWrapper>
  );
};

export default PoolWidget;
