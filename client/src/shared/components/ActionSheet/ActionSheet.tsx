import { Fragment, type ReactNode, useRef } from "react";
import clsx from "clsx";
import { createPortal } from "react-dom";

import Divider from "@/shared/components/Divider";
import Row from "@/shared/components/Row";
import { useClickOutsideDismiss } from "@/shared/hooks/useClickOutsideDismiss";
import { useEscapeDismiss } from "@/shared/hooks/useEscapeDismiss";

export interface ActionSheetItem {
  danger?: boolean;
  disabled?: boolean;
  icon?: ReactNode;
  label?: string;
  loading?: boolean;
  onClick?: () => void;
  showGroupDivider?: boolean;
  testId?: string;
}

interface ActionSheetProps {
  contentTestId?: string;
  items: ActionSheetItem[];
  onClose: () => void;
  testId?: string;
}

const ActionSheet = ({
  contentTestId = "action-sheet-content",
  items,
  onClose,
  testId = "action-sheet",
}: ActionSheetProps) => {
  const contentRef = useRef<HTMLDivElement>(null);

  useEscapeDismiss(onClose);
  useClickOutsideDismiss({ ref: contentRef, onDismiss: onClose });

  const visibleItems = items.filter((item) => item.label);
  const nonDangerItems = visibleItems.filter((item) => !item.danger);
  const dangerItems = visibleItems.filter((item) => item.danger);

  const renderItem = (item: ActionSheetItem, index: number, items: ActionSheetItem[], danger = false) => {
    const disabled = item.disabled || item.loading;

    return (
      <Fragment key={`${danger ? "danger-" : ""}${item.label}-${index}`}>
        <Row
          testId={item.testId}
          className={clsx(
            "text-emphasis-300",
            danger ? "text-intent-critical-fill" : "text-text-primary",
            disabled && "pointer-events-none opacity-40",
          )}
          disabled={disabled}
          prefixIcon={item.icon}
          onClick={
            disabled
              ? undefined
              : () => {
                  item.onClick?.();
                  onClose();
                }
          }
          divider={false}
        >
          {item.label}
        </Row>
        {item.showGroupDivider && index < items.length - 1 ? <Divider dividerStyle="thick" /> : null}
      </Fragment>
    );
  };

  return createPortal(
    <div
      className="fixed inset-0 z-60 flex items-end bg-grayscale-gray-5"
      data-testid={testId}
      role="presentation"
      onClick={(event) => {
        event.stopPropagation();
        onClose();
      }}
    >
      <div
        ref={contentRef}
        data-testid={contentTestId}
        className="max-h-[calc(100dvh-theme(spacing.10))] w-full overflow-y-auto overscroll-contain rounded-t-2xl bg-surface-elevated-base px-6 pt-2 pb-[max(env(safe-area-inset-bottom),16px)]"
        role="dialog"
        aria-modal="true"
        onClick={(event) => event.stopPropagation()}
      >
        {nonDangerItems.map((item, index) => renderItem(item, index, nonDangerItems))}

        {dangerItems.length > 0 && nonDangerItems.length > 0 ? <Divider /> : null}

        {dangerItems.map((item, index) => renderItem(item, index, dangerItems, true))}
      </div>
    </div>,
    document.body,
  );
};

export default ActionSheet;
