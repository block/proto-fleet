import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import clsx from "clsx";
import ViewModal, { type ViewModalState } from "./ViewModal";
import { useActiveSite } from "@/protoFleet/components/PageHeader/SitePicker";
import {
  buildUrlForView,
  canonicalizeSearchParams,
  findView,
  type FleetTabId,
  type SavedView,
  TABS_WITH_SAVEABLE_STATE,
  VIEW_URL_PARAM,
} from "@/protoFleet/features/fleetManagement/views/savedViews";
import type { UseFleetViewsResult } from "@/protoFleet/features/fleetManagement/views/useFleetViews";
import {
  type FilterSummaryContext,
  stripDisplayFromSearchParams,
  stripSortFromSearchParams,
  summarizeDisplay,
  summarizeFilters,
  summarizeSort,
} from "@/protoFleet/features/fleetManagement/views/viewSummary";
import { scopedPath } from "@/protoFleet/routing/siteScope";
import { ChevronDown, Dismiss, Edit, Ellipsis, Plus, Reboot, Trash } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Divider from "@/shared/components/Divider";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Radio from "@/shared/components/Radio";
import Row from "@/shared/components/Row";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type FleetViewTabsProps = {
  viewsState: UseFleetViewsResult;
  currentTab: FleetTabId | undefined;
  filterContext: FilterSummaryContext;
};

type KebabRowProps = {
  testId: string;
  onClick: () => void;
  icon: ReactNode;
  label: string;
  disabled?: boolean;
};

const KebabRow = ({ testId, onClick, icon, label, disabled }: KebabRowProps) => (
  <div className="px-4">
    <Row
      className="text-emphasis-300"
      testId={testId}
      onClick={onClick}
      disabled={disabled}
      compact
      divider={false}
      prefixIcon={icon}
    >
      {label}
    </Row>
  </div>
);

type ViewRowProps = {
  view: SavedView;
  isActive: boolean;
  onClick: () => void;
};

const ViewRow = ({ view, isActive, onClick }: ViewRowProps) => (
  <div className="px-4">
    <Row
      className={clsx("text-emphasis-300", isActive && "text-text-emphasis")}
      testId={`fleet-view-row-${view.id}`}
      onClick={onClick}
      compact
      divider={false}
      prefixIcon={<Radio selected={isActive} />}
    >
      {view.name}
    </Row>
  </div>
);

type FleetViewTabsInnerProps = FleetViewTabsProps;

const FleetViewTabsInner = ({ viewsState, currentTab, filterContext }: FleetViewTabsInnerProps) => {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const { activeSite } = useActiveSite({});
  const { record, addUserView, updateUserViewParams, renameUserView, deleteUserView } = viewsState;

  // Two popovers share one provider so click-outside on either dismisses both.
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [openPopover, setOpenPopover] = useState<"none" | "views" | "kebab">("none");

  // Portal both popovers out of the TabStrip's overflow-clip so they extend
  // past the strip edges without being cropped.
  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  useClickOutside({
    ref: triggerRef,
    onClickOutside: () => setOpenPopover("none"),
    ignoreSelectors: [".popover-content"],
  });

  const [modal, setModal] = useState<ViewModalState>({ open: false });
  const [deleteConfirm, setDeleteConfirm] = useState<
    { open: false } | { open: true; viewId: string; viewName: string }
  >({ open: false });

  const userViews = record.views;
  const hasViews = userViews.length > 0;
  const canSaveCurrentTab = currentTab !== undefined && TABS_WITH_SAVEABLE_STATE.has(currentTab);

  // Treat the URL view param as the active view *only* when its owning tab
  // matches the current section tab. A stale `view=` from another tab is
  // ignored so we never surface a dirty indicator on a view the user can't
  // be editing.
  const activeViewId = useMemo(() => {
    const param = searchParams.get(VIEW_URL_PARAM);
    if (!param) return undefined;
    const view = findView(param, record);
    if (!view || view.tab !== currentTab) return undefined;
    return view.id;
  }, [searchParams, record, currentTab]);
  const activeView = activeViewId ? findView(activeViewId, record) : undefined;

  const currentCanonical = useMemo(
    () => (currentTab ? canonicalizeSearchParams(searchParams, currentTab) : ""),
    [searchParams, currentTab],
  );
  const isDirty = activeView !== undefined && activeView.searchParams !== currentCanonical;

  const filterSummary = useMemo(
    () => (currentTab ? summarizeFilters(searchParams, currentTab, filterContext) : []),
    [searchParams, currentTab, filterContext],
  );
  const sortSummary = useMemo(
    () => (currentTab ? summarizeSort(searchParams, currentTab) : undefined),
    [searchParams, currentTab],
  );
  const displaySummary = useMemo(
    () => (currentTab ? summarizeDisplay(searchParams, currentTab) : undefined),
    [searchParams, currentTab],
  );

  const navigateToSavedView = useCallback(
    (view: SavedView, savedParams: string) => {
      const next = new URLSearchParams(savedParams);
      next.set(VIEW_URL_PARAM, view.id);
      navigate(scopedPath(`/fleet/${view.tab}?${next.toString()}`, activeSite), { replace: true });
    },
    [activeSite, navigate],
  );

  const handleSelectView = useCallback(
    (view: SavedView) => {
      const params = buildUrlForView(view, searchParams);
      navigate(scopedPath(`/fleet/${view.tab}?${params}`, activeSite));
      setOpenPopover("none");
    },
    [activeSite, navigate, searchParams],
  );

  const handleOpenNew = useCallback(() => {
    if (!currentTab) return;
    setOpenPopover("none");
    setModal({
      open: true,
      mode: { kind: "create", tab: currentTab },
      defaultName: "",
      currentFilters: filterSummary,
      currentSort: sortSummary,
      currentDisplay: displaySummary,
    });
  }, [currentTab, filterSummary, sortSummary, displaySummary]);

  const handleResetActiveView = useCallback(() => {
    if (!activeView) return;
    navigate(scopedPath(`/fleet/${activeView.tab}?${buildUrlForView(activeView, searchParams)}`, activeSite), {
      replace: true,
    });
  }, [activeSite, activeView, navigate, searchParams]);

  // Clear view: navigate to the bare tab route, dropping every URL key
  // (view=, the tab's filter/sort/display whitelist, and any unrelated
  // state). "Clear view" reads to operators as a full reset of the
  // section's URL, so a clean slate is intentional.
  const handleClearActiveView = useCallback(() => {
    if (!currentTab) return;
    navigate(scopedPath(`/fleet/${currentTab}`, activeSite));
  }, [activeSite, currentTab, navigate]);

  const handleOpenUpdateActiveView = useCallback(() => {
    if (!activeView) return;
    const savedParams = new URLSearchParams(activeView.searchParams);
    const savedFilters = summarizeFilters(savedParams, activeView.tab, filterContext);
    const savedSort = summarizeSort(savedParams, activeView.tab);
    const savedDisplay = summarizeDisplay(savedParams, activeView.tab);
    setModal({
      open: true,
      mode: {
        kind: "update",
        intent: "update",
        viewId: activeView.id,
        tab: activeView.tab,
        currentName: activeView.name,
        savedFilters,
        savedSort,
        savedDisplay,
      },
      defaultName: activeView.name,
      currentFilters: filterSummary,
      currentSort: sortSummary,
      currentDisplay: displaySummary,
    });
  }, [activeView, filterContext, filterSummary, sortSummary, displaySummary]);

  const handleOpenRenameActiveView = useCallback(() => {
    if (!activeView) return;
    const savedParams = new URLSearchParams(activeView.searchParams);
    const viewFilters = summarizeFilters(savedParams, activeView.tab, filterContext);
    const viewSort = summarizeSort(savedParams, activeView.tab);
    const viewDisplay = summarizeDisplay(savedParams, activeView.tab);
    // Rename is a name-only edit. The saved params stay frozen regardless of
    // current dirty state — handleSubmit branches on `intent === "rename"`
    // and skips updateUserViewParams entirely.
    setModal({
      open: true,
      mode: {
        kind: "update",
        intent: "rename",
        viewId: activeView.id,
        tab: activeView.tab,
        currentName: activeView.name,
        savedFilters: viewFilters,
        savedSort: viewSort,
        savedDisplay: viewDisplay,
      },
      defaultName: activeView.name,
      currentFilters: viewFilters,
      currentSort: viewSort,
      currentDisplay: viewDisplay,
    });
  }, [activeView, filterContext]);

  const handleOpenDeleteActiveView = useCallback(() => {
    if (!activeView) return;
    setDeleteConfirm({ open: true, viewId: activeView.id, viewName: activeView.name });
  }, [activeView]);

  const handleConfirmDelete = useCallback(() => {
    if (!deleteConfirm.open) return;
    const idToDelete = deleteConfirm.viewId;
    deleteUserView(idToDelete);
    setDeleteConfirm({ open: false });
    if (activeViewId === idToDelete) {
      const next = new URLSearchParams(searchParams);
      next.delete(VIEW_URL_PARAM);
      const nextSearch = next.toString();
      navigate({ search: nextSearch ? `?${nextSearch}` : "" }, { replace: true });
    }
  }, [deleteConfirm, deleteUserView, activeViewId, navigate, searchParams]);

  const handleSubmit = useCallback(
    ({ name, includeSort, includeDisplay }: { name: string; includeSort: boolean; includeDisplay: boolean }) => {
      if (!modal.open) return;
      // Apply the includeSort + includeDisplay toggles by stripping the
      // corresponding URL keys before persisting. Strip orders don't matter
      // — they target disjoint param sets.
      const applyToggles = (params: string): string => {
        let next = params;
        if (!includeSort) next = stripSortFromSearchParams(next);
        if (!includeDisplay) next = stripDisplayFromSearchParams(next);
        return next;
      };
      if (modal.mode.kind === "update") {
        const { intent, viewId: targetId, tab: targetTab } = modal.mode;
        const target = record.views.find((view) => view.id === targetId);
        if (!target) {
          setModal({ open: false });
          return;
        }
        if (intent === "rename") {
          // Name-only edit; saved params are frozen and the URL is not
          // touched. Dirty state — if any — persists, intentionally.
          if (name !== target.name) {
            renameUserView(targetId, name);
          }
          setModal({ open: false });
          return;
        }
        const baseCanonical =
          targetId === activeViewId && currentTab === targetTab ? currentCanonical : target.searchParams;
        const paramsForView = applyToggles(baseCanonical);
        updateUserViewParams(targetId, paramsForView);
        if (name !== target.name) {
          renameUserView(targetId, name);
        }
        if (targetId === activeViewId) {
          navigateToSavedView({ ...target, name, searchParams: paramsForView }, paramsForView);
        }
      } else {
        const tab = modal.mode.tab;
        const paramsForView = applyToggles(currentCanonical);
        const newView = addUserView({ name, tab, searchParams: paramsForView });
        navigateToSavedView(newView, paramsForView);
      }
      setModal({ open: false });
    },
    [
      modal,
      record.views,
      activeViewId,
      currentTab,
      addUserView,
      currentCanonical,
      navigateToSavedView,
      renameUserView,
      updateUserViewParams,
    ],
  );

  const existingNames = useMemo(() => {
    if (modal.open && modal.mode.kind === "update") {
      const editingId = modal.mode.viewId;
      return userViews.filter((view) => view.id !== editingId).map((view) => view.name);
    }
    return userViews.map((view) => view.name);
  }, [userViews, modal]);

  // Anchor both popovers off the same wrapper so they hang below the button
  // row consistently and the click-outside boundary covers both triggers.
  const wrapperRef = useCallback(
    (node: HTMLDivElement | null) => {
      triggerRef.current = node;
    },
    [triggerRef],
  );

  // Text controls in the strip read as part of the tab-nav typography:
  // same `text-300` size as TabStripItem. The `pb-2` baseline offset (which
  // aligns text with TabStripItem's underline) is applied at the wrapper
  // level so the kebab Button — symmetric padding, no `pb-2` of its own —
  // can vertically center against the visible text rather than slipping
  // below an asymmetrically padded text element.
  const tabTextBase =
    "inline-flex items-center gap-1 text-300 outline-none focus-visible:underline disabled:cursor-not-allowed disabled:opacity-50";
  const tabTextIdle = "text-text-primary-70 hover:text-text-primary";
  const tabTextActive = "text-text-emphasis";

  // Empty state: no saved views. Just the "+ New view" text trigger.
  if (!hasViews) {
    return (
      <>
        <div ref={wrapperRef}>
          <button
            type="button"
            onClick={handleOpenNew}
            disabled={!canSaveCurrentTab}
            className={clsx(tabTextBase, tabTextIdle)}
            data-testid="fleet-view-tabs-new-view-button"
          >
            <Plus width={iconSizes.xSmall} />
            <span>New view</span>
          </button>
        </div>
        <ViewModal
          state={modal}
          existingNames={existingNames}
          onDismiss={() => setModal({ open: false })}
          onSubmit={handleSubmit}
        />
      </>
    );
  }

  const triggerLabel = activeView?.name ?? "My views";
  const isViewsOpen = openPopover === "views";
  const isKebabOpen = openPopover === "kebab";

  // Trigger text color mirrors active/dirty state. Kebab Ellipsis reuses the
  // same token so the two controls always read as one unit — dirty warning
  // on the trigger means warning on the kebab too.
  const triggerColorClass = isDirty
    ? "text-intent-warning-50 hover:text-intent-warning-50"
    : activeView
      ? tabTextActive
      : tabTextIdle;
  const kebabIconColorClass = isDirty
    ? "text-intent-warning-50"
    : activeView
      ? "text-text-emphasis"
      : "text-text-primary-70";

  return (
    <>
      <div ref={wrapperRef} className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => setOpenPopover((prev) => (prev === "views" ? "none" : "views"))}
          aria-haspopup="menu"
          aria-expanded={isViewsOpen}
          className={clsx(tabTextBase, triggerColorClass)}
          data-testid="fleet-view-tabs-trigger"
        >
          <span>{triggerLabel}</span>
          <div className={clsx("transition-transform duration-200", { "rotate-180": isViewsOpen })}>
            <ChevronDown width={iconSizes.xSmall} />
          </div>
        </button>
        {activeView ? (
          // Match the kebab Button used in the miner-list name column
          // (RowActionsMenu): textOnly, compact, Ellipsis at iconSizes.small,
          // with the same `-my-[10px] !p-[14px]` hit-area override so the
          // tap target stays generous without enlarging the rendered glyph.
          <Button
            className="-my-[10px] !p-[14px]"
            size={sizes.compact}
            variant={variants.textOnly}
            prefixIcon={<Ellipsis width={iconSizes.small} className={kebabIconColorClass} />}
            ariaLabel={`Actions for ${activeView.name}`}
            ariaHasPopup="menu"
            ariaExpanded={isKebabOpen}
            onClick={() => setOpenPopover((prev) => (prev === "kebab" ? "none" : "kebab"))}
            testId="fleet-view-tabs-kebab"
          />
        ) : null}
      </div>

      {isViewsOpen ? (
        <Popover
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId="fleet-view-tabs-views-popover"
        >
          {userViews.map((view) => (
            <ViewRow
              key={view.id}
              view={view}
              isActive={view.id === activeViewId}
              onClick={() => handleSelectView(view)}
            />
          ))}
          <div className="my-1">
            <Divider />
          </div>
          <KebabRow
            testId="fleet-view-tabs-popover-new-view"
            onClick={handleOpenNew}
            disabled={!canSaveCurrentTab}
            icon={<Plus width={iconSizes.xSmall} />}
            label="New view"
          />
          {activeView ? (
            <KebabRow
              testId="fleet-view-tabs-popover-clear-view"
              onClick={() => {
                setOpenPopover("none");
                handleClearActiveView();
              }}
              icon={<Dismiss width={iconSizes.xSmall} />}
              label="Clear view"
            />
          ) : null}
        </Popover>
      ) : null}

      {isKebabOpen && activeView ? (
        <Popover
          className="!space-y-0 !rounded-2xl px-0 pt-2 pb-1"
          position={positions["bottom right"]}
          size={popoverSizes.small}
          offset={8}
          testId="fleet-view-tabs-kebab-popover"
        >
          {isDirty ? (
            <KebabRow
              testId="fleet-view-tabs-reset-action"
              onClick={() => {
                setOpenPopover("none");
                handleResetActiveView();
              }}
              icon={<Reboot />}
              label="Reset view"
            />
          ) : null}
          {isDirty ? (
            <KebabRow
              testId="fleet-view-tabs-update-action"
              onClick={() => {
                setOpenPopover("none");
                handleOpenUpdateActiveView();
              }}
              icon={<Edit />}
              label="Update view"
            />
          ) : null}
          <KebabRow
            testId="fleet-view-tabs-rename-action"
            onClick={() => {
              setOpenPopover("none");
              handleOpenRenameActiveView();
            }}
            icon={<Edit />}
            label="Rename view"
          />
          <KebabRow
            testId="fleet-view-tabs-delete-action"
            onClick={() => {
              setOpenPopover("none");
              handleOpenDeleteActiveView();
            }}
            icon={<Trash />}
            label="Delete view"
          />
        </Popover>
      ) : null}

      <ViewModal
        state={modal}
        existingNames={existingNames}
        onDismiss={() => setModal({ open: false })}
        onSubmit={handleSubmit}
      />

      <Dialog
        open={deleteConfirm.open}
        title="Delete view"
        onDismiss={() => setDeleteConfirm({ open: false })}
        testId="fleet-view-tabs-delete-dialog"
        buttons={[
          { text: "Cancel", onClick: () => setDeleteConfirm({ open: false }), variant: variants.secondary },
          { text: "Delete", onClick: handleConfirmDelete, variant: variants.danger },
        ]}
      >
        <div className="text-300 text-text-primary-70">
          {deleteConfirm.open ? `Delete the view "${deleteConfirm.viewName}"? This can't be undone.` : null}
        </div>
      </Dialog>
    </>
  );
};

/**
 * Renders the saved-views dropdown trigger (and adjacent kebab when a view
 * is active) for the FleetLayout top tab row. Designed to live in the
 * TabStrip's `trailing` slot so it sits right-aligned across from the
 * section tabs.
 *
 * Visuals: trigger renders as a text-style button with tab-nav typography
 * (`text-300`, color tokens that mirror TabStripItem). It reads as part of
 * the tab row rather than as a competing action button.
 */
const FleetViewTabs = (props: FleetViewTabsProps) => (
  <PopoverProvider>
    <FleetViewTabsInner {...props} />
  </PopoverProvider>
);

export default FleetViewTabs;
