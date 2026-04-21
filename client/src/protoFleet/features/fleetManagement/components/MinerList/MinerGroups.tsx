import { useCallback, useRef } from "react";
import { Link } from "react-router-dom";
import { createPortal } from "react-dom";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

type MinerGroupsProps = {
  miner: MinerStateSnapshot;
  availableGroups: DeviceSet[];
};

const MinerGroups = ({ miner, availableGroups }: MinerGroupsProps) => {
  const groupLabels = miner.groupLabels;
  const { triggerRef, floatingStyle, isVisible, show, hide } = useFloatingPosition<HTMLSpanElement>({
    placement: "bottom-start",
    maxHeight: 400,
    minWidth: 240,
  });
  const closeTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  const open = useCallback(() => {
    if (closeTimeout.current) {
      clearTimeout(closeTimeout.current);
      closeTimeout.current = null;
    }
    show();
  }, [show]);

  const closeWithDelay = useCallback(() => {
    closeTimeout.current = setTimeout(() => {
      hide();
    }, 100);
  }, [hide]);

  if (!groupLabels || groupLabels.length === 0) {
    return <span />;
  }

  const getGroupLink = (label: string) => {
    const groupId = availableGroups.find((g) => g.label === label)?.id;
    return groupId ? `/groups/${encodeURIComponent(label)}` : undefined;
  };

  if (groupLabels.length === 1) {
    const link = getGroupLink(groupLabels[0]);
    return link ? (
      <Link to={link} className="hover:underline">
        {groupLabels[0]}
      </Link>
    ) : (
      <span>{groupLabels[0]}</span>
    );
  }

  return (
    <span ref={triggerRef} className="cursor-default" onMouseEnter={open} onMouseLeave={closeWithDelay}>
      {groupLabels.length} groups
      {isVisible &&
        createPortal(
          <div
            className="fixed z-[9999] min-w-60 rounded-lg bg-surface-elevated-base px-3 py-2 shadow-lg"
            style={floatingStyle}
            onMouseEnter={open}
            onMouseLeave={closeWithDelay}
          >
            <ul className="flex flex-col divide-y divide-border-5 whitespace-nowrap">
              {groupLabels.map((label) => {
                const link = getGroupLink(label);
                return (
                  <li key={label} className="py-2">
                    {link ? (
                      <Link to={link} className="text-300 hover:underline">
                        {label}
                      </Link>
                    ) : (
                      <span>{label}</span>
                    )}
                  </li>
                );
              })}
            </ul>
          </div>,
          document.body,
        )}
    </span>
  );
};

export default MinerGroups;
