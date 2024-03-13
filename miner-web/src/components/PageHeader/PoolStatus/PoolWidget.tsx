import { useMemo } from "react";
import clsx from "clsx";

import { Pool } from "apiTypes";

import Spinner from "components/Spinner";

import "./style.css";

interface PoolWidgetProps {
  loading?: boolean;
  isConnected?: boolean;
  onTogglePopover: () => void;
  poolInfo?: Pick<Pool, "priority" | "status" | "url">;
}

const PoolWidget = ({
  loading = false,
  isConnected = false,
  onTogglePopover,
  poolInfo,
}: PoolWidgetProps) => {
  const isDisconnected = useMemo(() => !isConnected && !loading, [isConnected, loading]);
  const poolConfigured = useMemo(() => poolInfo?.url, [poolInfo]);

  const statusLabel = useMemo(() => {
    if (loading) {
      return "Connecting";
    }
    if (isConnected) {
      return "Connected";
    }
    if (isDisconnected) {
      return poolConfigured ? "Disconnected" : "No pools configured";
    }
  }, [isConnected, isDisconnected, loading, poolConfigured]);

  return (
    <button
      className={clsx(
        "rounded text-heading-50 flex",
        { "hover:cursor-progress": loading },
        { "shadow-50 text-text-primary/50": isConnected || loading },
        { disconnected: isDisconnected }
      )}
      onClick={loading ? undefined : onTogglePopover}
    >
      <div
        className={clsx("px-2 py-1 rounded-s", {
          "bg-surface-5": isConnected || loading,
          "bg-intent-critical-fill/20 text-intent-critical-text":
            isDisconnected,
        })}
      >
        Pool
      </div>
      <div
        className={clsx("px-2 py-1 flex items-center rounded-e", {
          "bg-intent-critical-fill/10 text-intent-critical-text":
            isDisconnected,
        })}
      >
        {loading ? (
          <Spinner className="mr-1" size={14} />
        ) : (
          <svg
            width="6"
            height="6"
            viewBox="0 0 6 6"
            className={clsx("mr-1", {
              "text-intent-success-fill": isConnected || loading,
              "text-intent-critical-text": isDisconnected,
            })}
          >
            <circle cx="3" cy="3" r="3" fill="currentColor" />
          </svg>
        )}
        {statusLabel}
      </div>
    </button>
  );
};

export default PoolWidget;
