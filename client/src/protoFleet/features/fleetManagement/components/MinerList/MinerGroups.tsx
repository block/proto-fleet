import { useCallback, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useMinerGroupLabels } from "@/protoFleet/store";

type MinerGroupsProps = {
  deviceIdentifier: string;
};

const MinerGroups = ({ deviceIdentifier }: MinerGroupsProps) => {
  const groupLabels = useMinerGroupLabels(deviceIdentifier);
  const triggerRef = useRef<HTMLSpanElement>(null);
  const [popoverRect, setPopoverRect] = useState<DOMRect | null>(null);
  const closeTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);

  const open = useCallback(() => {
    if (closeTimeout.current) {
      clearTimeout(closeTimeout.current);
      closeTimeout.current = null;
    }
    const rect = triggerRef.current?.getBoundingClientRect();
    if (rect) setPopoverRect(rect);
  }, []);

  const closeWithDelay = useCallback(() => {
    closeTimeout.current = setTimeout(() => {
      setPopoverRect(null);
    }, 100);
  }, []);

  if (!groupLabels || groupLabels.length === 0) {
    return <span />;
  }

  if (groupLabels.length === 1) {
    return <span>{groupLabels[0]}</span>;
  }

  return (
    <span ref={triggerRef} className="cursor-default" onMouseEnter={open} onMouseLeave={closeWithDelay}>
      {groupLabels.length} groups
      {popoverRect &&
        createPortal(
          <div
            className="fixed z-[9999] min-w-60 rounded-lg px-3 py-2 shadow-lg"
            style={{ top: popoverRect.bottom + 4, left: popoverRect.left, background: "#fff" }}
            onMouseEnter={open}
            onMouseLeave={closeWithDelay}
          >
            <ul className="flex flex-col divide-y divide-border-5 whitespace-nowrap">
              {groupLabels.map((label) => (
                <li key={label} className="py-2">
                  <a href="#" className="text-emphasis-300 hover:underline">
                    {label}
                  </a>
                </li>
              ))}
            </ul>
          </div>,
          document.body,
        )}
    </span>
  );
};

export default MinerGroups;
