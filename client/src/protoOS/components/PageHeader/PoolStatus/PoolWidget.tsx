import clsx from "clsx";

import WidgetWrapper from "../WidgetWrapper";

interface PoolWidgetProps {
  loading: boolean;
  isConnected: boolean;
  isOpen: boolean;
  onTogglePopover: () => void;
}

const PoolWidget = ({ loading, isOpen, onTogglePopover }: PoolWidgetProps) => {
  return (
    <WidgetWrapper
      onClick={loading ? undefined : onTogglePopover}
      className={clsx("text-text-primary", {
        "hover:cursor-progress": loading,
      })}
      isOpen={isOpen}
    >
      Mining Pool
    </WidgetWrapper>
  );
};

export default PoolWidget;
