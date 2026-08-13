import { Fragment, useCallback, useEffect, useState } from "react";
import clsx from "clsx";

import { buildModuleActions, type ModuleActionType } from "./moduleActions";
import type { TankModuleState } from "./TankModuleGrid";
import ActionSheet from "@/shared/components/ActionSheet";
import Divider from "@/shared/components/Divider";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

interface ModuleTileProps {
  /** Composite health of the module this tile represents. */
  state: TankModuleState;
  label: string;
  /**
   * Fired with the chosen action when the caller wires an interactive tile.
   * When omitted the tile is a plain, non-interactive status bar.
   */
  onAction?: (type: ModuleActionType) => void;
}

/**
 * A single tank module bar. When `onAction` is supplied the bar becomes the
 * trigger for a popover action menu (View + Blink LEDs / Reboot / Sleep),
 * reusing the same shared Popover / ActionSheet / Row primitives the fleet
 * RowActionsMenu uses — a desktop Popover and a phone ActionSheet. The menu is
 * anchored to the tile itself rather than a separate ellipsis affordance.
 */
const ModuleBar = ({ state }: { state: TankModuleState }) => {
  const attention = state === "attention";
  return (
    <>
      {/* Attention modules read as three equal-width cells: a bright centre
          third over the light body. */}
      {attention ? <span aria-hidden className="absolute inset-y-0 left-1/3 w-1/3 bg-core-accent-fill" /> : null}
    </>
  );
};

const barClassName = (state: TankModuleState) =>
  clsx(
    "relative aspect-[4/7] overflow-hidden rounded-md",
    state === "attention" ? "bg-core-accent-50" : "bg-core-primary-10",
  );

const ModuleTileInner = ({
  state,
  label,
  onAction,
}: Required<Pick<ModuleTileProps, "state" | "label">> & Pick<ModuleTileProps, "onAction">) => {
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const { isPhone } = useWindowDimensions();
  const [isOpen, setIsOpen] = useState(false);
  const popoverTestId = "module-actions-popover";

  // Portal-fixed keeps the menu above the tank card's overflow-hidden bounds.
  useEffect(() => {
    setPopoverRenderMode("portal-fixed");
  }, [setPopoverRenderMode]);

  const setMenuOpen = useCallback((next: boolean) => setIsOpen(next), []);
  const onClickOutside = useCallback(() => setMenuOpen(false), [setMenuOpen]);
  useClickOutside({
    ref: triggerRef,
    onClickOutside,
    ignoreSelectors: [".popover-content", `[data-testid="${popoverTestId}"]`],
    enabled: isOpen,
  });

  const actions = buildModuleActions((type) => {
    setMenuOpen(false);
    onAction?.(type);
  });

  return (
    <div className="relative" ref={triggerRef}>
      <button
        type="button"
        data-testid="tank-module"
        data-module-state={state}
        aria-label={`${label} actions`}
        aria-haspopup="menu"
        aria-expanded={isOpen}
        onClick={() => setMenuOpen(!isOpen)}
        className={clsx(
          barClassName(state),
          "w-full cursor-pointer border-0 p-0 transition-opacity outline-none hover:opacity-80",
          "focus-visible:ring-2 focus-visible:ring-core-primary-fill focus-visible:ring-offset-2 focus-visible:ring-offset-surface-base",
        )}
      >
        <ModuleBar state={state} />
      </button>
      {isOpen && isPhone ? (
        <ActionSheet
          items={actions.map((action) => ({
            icon: action.icon,
            label: action.label,
            onClick: action.onClick,
            showGroupDivider: action.showGroupDivider,
            testId: action.testId,
          }))}
          onClose={() => setMenuOpen(false)}
          contentTestId={popoverTestId}
          testId={`${popoverTestId}-sheet`}
        />
      ) : null}
      {isOpen && !isPhone ? (
        <Popover
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          constrainHeightToViewport
          closeIgnoreSelectors={[`[data-testid="tank-module"]`]}
          closePopover={() => setMenuOpen(false)}
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId={popoverTestId}
        >
          {actions.map((action, index) => (
            <Fragment key={action.testId ?? `${action.label}-${index}`}>
              <div className="px-4">
                <Row
                  className="text-emphasis-300"
                  prefixIcon={action.icon}
                  testId={action.testId}
                  onClick={action.onClick}
                  compact
                  divider={false}
                >
                  {action.label}
                </Row>
              </div>
              {action.showGroupDivider && index < actions.length - 1 ? <Divider dividerStyle="thick" /> : null}
            </Fragment>
          ))}
        </Popover>
      ) : null}
    </div>
  );
};

const ModuleTile = ({ state, label, onAction }: ModuleTileProps) => {
  // A non-interactive tile is just the status bar — no popover machinery.
  if (!onAction) {
    return (
      <div className={barClassName(state)} data-testid="tank-module" data-module-state={state}>
        <ModuleBar state={state} />
      </div>
    );
  }

  return (
    <PopoverProvider>
      <ModuleTileInner state={state} label={label} onAction={onAction} />
    </PopoverProvider>
  );
};

export default ModuleTile;
export type { ModuleTileProps };
