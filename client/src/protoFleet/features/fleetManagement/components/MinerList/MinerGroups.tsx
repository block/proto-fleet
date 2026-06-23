import { useCallback, useRef } from "react";
import { Link } from "react-router-dom";
import { createPortal } from "react-dom";
import { type DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { getMinerGroupRefs } from "@/protoFleet/features/fleetManagement/utils/minerPlacement";
import { scopedPath, useRouteSiteScope } from "@/protoFleet/routing/siteScope";
import { DEFAULT_ACTIVE_SITE } from "@/protoFleet/store/types/activeSite";
import { useFloatingPosition } from "@/shared/hooks/useFloatingPosition";

type MinerGroupsProps = {
  miner: MinerStateSnapshot;
  availableGroups: DeviceSet[];
};

const MinerGroups = ({ miner, availableGroups }: MinerGroupsProps) => {
  const activeSite = useRouteSiteScope() ?? DEFAULT_ACTIVE_SITE;
  const groupRefs = getMinerGroupRefs(miner);
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

  if (groupRefs.length === 0) {
    return <span />;
  }

  const getGroupLink = (groupId: bigint | undefined, label: string) => {
    const id = groupId ?? availableGroups.find((g) => g.label === label)?.id;
    return id ? scopedPath(`/groups/${encodeURIComponent(label)}`, activeSite) : undefined;
  };

  if (groupRefs.length === 1) {
    const group = groupRefs[0];
    const link = getGroupLink(group.id, group.label);
    return link ? (
      <Link to={link} className="hover:underline">
        {group.label}
      </Link>
    ) : (
      <span>{group.label}</span>
    );
  }

  return (
    <span ref={triggerRef} className="cursor-default" onMouseEnter={open} onMouseLeave={closeWithDelay}>
      {groupRefs.length} groups
      {isVisible
        ? createPortal(
            <div
              className="fixed z-[9999] min-w-60 rounded-lg bg-surface-elevated-base px-3 py-2 shadow-lg"
              style={floatingStyle}
              onMouseEnter={open}
              onMouseLeave={closeWithDelay}
            >
              <ul className="flex flex-col divide-y divide-border-5 whitespace-nowrap">
                {groupRefs.map((group) => {
                  const link = getGroupLink(group.id, group.label);
                  const key = `${group.id}-${group.label}`;
                  return (
                    <li key={key} className="py-2">
                      {link ? (
                        <Link to={link} className="text-300 hover:underline">
                          {group.label}
                        </Link>
                      ) : (
                        <span>{group.label}</span>
                      )}
                    </li>
                  );
                })}
              </ul>
            </div>,
            document.body,
          )
        : null}
    </span>
  );
};

export default MinerGroups;
