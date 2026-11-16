import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronUpDown } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Popover, { usePopover } from "@/shared/components/Popover";
import SkeletonBar from "@/shared/components/SkeletonBar";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";
import useMeasure from "@/shared/hooks/useMeasure";

const headingClasses = {
  compact: { trigger: "!text-heading-200", skeleton: "text-heading-200" },
  base: { trigger: "!text-heading-300", skeleton: "text-heading-300" },
} as const;

type InlineSelectorProps = {
  items: { id: string; name: string }[];
  currentId?: string;
  onSelect: (id: string) => void;
  placement?: "below" | "right";
  headingSize?: keyof typeof headingClasses;
  disabled?: boolean;
};

const InlineSelector = ({
  items,
  currentId,
  onSelect,
  placement = "below",
  headingSize = "base",
  disabled = false,
}: InlineSelectorProps) => {
  const headingClass = headingClasses[headingSize];
  const [showPopover, setShowPopover] = useState<boolean>(false);
  const popoverRef = useRef<HTMLDivElement>(null);
  const { triggerRef } = usePopover();
  const [measureTrigger, , triggerRect] = useMeasure<HTMLDivElement>({ observeMutations: false });
  const setTrigger = useCallback(
    (element: HTMLDivElement | null) => {
      triggerRef.current = element;
      measureTrigger(element);
    },
    [triggerRef, measureTrigger],
  );
  useClickOutside({
    ref: popoverRef,
    onClickOutside: () => setShowPopover(false),
  });

  useEscapeDismiss(
    showPopover
      ? () => {
          setShowPopover(false);
          triggerRef.current?.querySelector("button")?.focus();
        }
      : undefined,
  );
  useEffect(() => {
    if (showPopover)
      popoverRef.current?.querySelector<HTMLButtonElement>('button[aria-pressed="true"], button')?.focus();
  }, [showPopover]);

  const currentIdName = useMemo(() => {
    const current = items.find((item) => item.id === currentId);
    return current?.name;
  }, [currentId, items]);

  const hasItems = items.length > 0 && !disabled;
  if (!hasItems && showPopover) setShowPopover(false);

  return (
    <div ref={setTrigger} className="relative w-fit">
      <Button
        textColor="text-text-primary"
        className={`!p-0 font-medium ${headingClass.trigger}`}
        variant="textOnly"
        suffixIcon={currentIdName ? <ChevronUpDown /> : undefined}
        ariaExpanded={hasItems ? showPopover : false}
        disabled={!hasItems}
        onClick={() => setShowPopover((prev) => !prev)}
      >
        {currentIdName || <SkeletonBar className={`w-25 ${headingClass.skeleton}`} />}
      </Button>

      {hasItems && showPopover ? (
        <Popover
          position="bottom right"
          xOffset={placement === "right" ? triggerRect.width + 8 : 0}
          yOffset={placement === "right" ? -triggerRect.height - 8 : 0}
          className="!space-y-0 px-0 py-0"
        >
          <div ref={popoverRef} data-inline-selector-menu className="flex max-h-80 flex-col overflow-y-auto px-3 py-2">
            {items.map((item) => (
              <button
                key={item.id}
                type="button"
                aria-pressed={currentId === item.id}
                className="flex items-center justify-between gap-6 rounded-lg px-3 py-3 text-left text-text-primary hover:bg-core-primary-5"
                onClick={() => {
                  onSelect(item.id);
                  setShowPopover(false);
                  triggerRef.current?.querySelector("button")?.focus({ preventScroll: true });
                }}
              >
                {item.name}
                <span
                  aria-hidden="true"
                  className={`h-4 w-4 rounded-full border border-border-20 ${currentId === item.id ? "bg-core-primary-fill" : ""}`}
                />
              </button>
            ))}
          </div>
        </Popover>
      ) : null}
    </div>
  );
};

export default InlineSelector;
