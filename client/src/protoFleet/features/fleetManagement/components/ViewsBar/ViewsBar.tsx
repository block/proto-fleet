import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import clsx from "clsx";
import {
  closestCenter,
  DndContext,
  type DragEndEvent,
  KeyboardSensor,
  type Modifier,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  horizontalListSortingStrategy,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import ViewModal, { type ViewModalState } from "./ViewModal";
import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  ALL_MINERS_VIEW_ID,
  buildUrlForView,
  canonicalizeSearchParams,
  findView,
  type SavedView,
  VIEW_URL_PARAM,
  visibleBuiltInViews,
} from "@/protoFleet/features/fleetManagement/views/savedViews";
import type { UseMinerViewsResult } from "@/protoFleet/features/fleetManagement/views/useMinerViews";
import {
  stripSortFromSearchParams,
  summarizeFilters,
  summarizeSort,
} from "@/protoFleet/features/fleetManagement/views/viewSummary";
import { Edit, Ellipsis, Plus, Trash } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Dialog from "@/shared/components/Dialog";
import Popover, { PopoverProvider, popoverSizes, usePopover } from "@/shared/components/Popover";
import Row from "@/shared/components/Row";
import { TabStrip, TabStripItem } from "@/shared/components/Tab";
import { positions } from "@/shared/constants";
import { useClickOutside } from "@/shared/hooks/useClickOutside";

type ViewsBarProps = {
  viewsState: UseMinerViewsResult;
  availableGroups: DeviceSet[];
  availableRacks: DeviceSet[];
  className?: string;
};

/**
 * Pin the drag transform to the X axis. Without this dnd-kit lets the dragged
 * tab move vertically too, which causes the tablist to scroll/flicker when the
 * pointer wanders below the row.
 */
const restrictToHorizontalAxis: Modifier = ({ transform }) => ({ ...transform, y: 0 });

type BuiltInTabProps = {
  view: SavedView;
  isActive: boolean;
  isDirty: boolean;
};

const BuiltInTab = ({ view, isActive, isDirty }: BuiltInTabProps) => (
  <TabStripItem
    id={view.id}
    testId={`views-bar-tab-${view.id}`}
    label={view.name}
    tone={isActive && isDirty ? "warning" : "default"}
  />
);

type ViewKebabProps = {
  view: SavedView;
  isOpen: boolean;
  setIsOpen: (next: boolean | ((prev: boolean) => boolean)) => void;
  onRename: (view: SavedView) => void;
  onDelete: (view: SavedView) => void;
};

const ViewKebab = ({ view, isOpen, setIsOpen, onRename, onDelete }: ViewKebabProps) => (
  <>
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation();
        setIsOpen((prev) => !prev);
      }}
      className="flex items-center justify-center px-1 pb-2 text-text-primary-70 outline-none hover:text-text-primary focus-visible:underline"
      aria-label={`Actions for ${view.name}`}
      aria-haspopup="menu"
      data-testid={`views-bar-tab-${view.id}-kebab`}
    >
      <Ellipsis width={iconSizes.xSmall} />
    </button>
    {isOpen ? (
      <Popover
        className="!space-y-0 px-4 pt-2 pb-1"
        position={positions["bottom right"]}
        size={popoverSizes.small}
        offset={8}
        testId={`views-bar-tab-${view.id}-kebab-popover`}
      >
        <Row
          className="text-emphasis-300"
          testId={`views-bar-tab-${view.id}-rename-action`}
          onClick={() => {
            setIsOpen(false);
            onRename(view);
          }}
          compact
          divider
          prefixIcon={<Edit width={iconSizes.small} className="text-text-primary-70" />}
        >
          Rename
        </Row>
        <Row
          className="text-emphasis-300"
          testId={`views-bar-tab-${view.id}-delete-action`}
          onClick={() => {
            setIsOpen(false);
            onDelete(view);
          }}
          compact
          divider={false}
          prefixIcon={<Trash width={iconSizes.small} className="text-text-primary-70" />}
        >
          Delete
        </Row>
      </Popover>
    ) : null}
  </>
);

type UserTabProps = {
  view: SavedView;
  isActive: boolean;
  isDirty: boolean;
  onRename: (view: SavedView) => void;
  onDelete: (view: SavedView) => void;
};

const UserTabInner = ({ view, isActive, isDirty, onRename, onDelete }: UserTabProps) => {
  const { setNodeRef, attributes, listeners, transform, transition, isDragging } = useSortable({ id: view.id });
  const { triggerRef, setPopoverRenderMode } = usePopover();
  const [isMenuOpen, setIsMenuOpen] = useState(false);

  // Tabs render inside an `overflow-x-auto` container, which clips an inline
  // popover. Portal it out so the menu can extend past the strip.
  useEffect(() => {
    setPopoverRenderMode("portal-scrolling");
  }, [setPopoverRenderMode]);

  useClickOutside({
    ref: triggerRef,
    onClickOutside: () => setIsMenuOpen(false),
    ignoreSelectors: [".popover-content"],
  });

  // Anchor the popover to the TabStripItem cell, not the kebab button — gives
  // the menu a wider, more predictable position relative to the visible tab.
  const wrapperRef = useCallback(
    (node: HTMLDivElement | null) => {
      setNodeRef(node);
      triggerRef.current = node;
    },
    [setNodeRef, triggerRef],
  );

  // Use Translate (not Transform) so dnd-kit doesn't apply scaleX/scaleY when
  // sibling tabs have different widths — that's what causes the dragged tab to
  // look stretched or squished. While dragging we also paint an opaque surface
  // background so the floating chip cleanly covers the tabs it passes over.
  return (
    <TabStripItem
      id={view.id}
      testId={`views-bar-tab-${view.id}`}
      label={view.name}
      tone={isActive && isDirty ? "warning" : "default"}
      trailing={
        <ViewKebab view={view} isOpen={isMenuOpen} setIsOpen={setIsMenuOpen} onRename={onRename} onDelete={onDelete} />
      }
      wrapperRef={wrapperRef}
      wrapperStyle={{
        transform: CSS.Translate.toString(transform),
        transition,
        cursor: isDragging ? "grabbing" : "grab",
      }}
      wrapperProps={{
        ...attributes,
        ...listeners,
        className: clsx("touch-none", isDragging && "z-10 bg-surface-base shadow-100"),
      }}
    />
  );
};

const UserTab = (props: UserTabProps) => (
  <PopoverProvider>
    <UserTabInner {...props} />
  </PopoverProvider>
);

type TabActionProps = {
  text: string;
  onClick: () => void;
  testId: string;
  prefixIcon?: ReactNode;
};

/**
 * Tab-styled action: same typography and underline alignment as TabStripItem,
 * but click triggers an action rather than activating a tab.
 */
const TabAction = ({ text, onClick, testId, prefixIcon }: TabActionProps) => (
  <button
    type="button"
    onClick={onClick}
    className="relative inline-flex items-center gap-1 px-2 pb-2 text-300 text-text-primary-70 outline-none hover:text-text-primary focus-visible:underline"
    data-testid={testId}
  >
    {prefixIcon}
    <span>{text}</span>
  </button>
);

const ViewsBar = ({ viewsState, availableGroups, availableRacks, className }: ViewsBarProps) => {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const { record, addUserView, reorderUserViews, updateUserViewParams, renameUserView, deleteUserView } = viewsState;

  const [modal, setModal] = useState<ViewModalState>({ open: false });
  const [deleteConfirm, setDeleteConfirm] = useState<
    { open: false } | { open: true; viewId: string; viewName: string }
  >({ open: false });

  const builtIns = useMemo(() => visibleBuiltInViews(record), [record]);
  const userViews = record.views;

  const currentCanonical = useMemo(() => canonicalizeSearchParams(searchParams), [searchParams]);
  // No `view=` param + no filters/sort → implicitly the All miners view, so
  // a fresh URL like "/" still highlights All miners and clicking it is a no-op.
  const activeViewId = searchParams.get(VIEW_URL_PARAM) ?? (currentCanonical === "" ? ALL_MINERS_VIEW_ID : undefined);
  const activeView = activeViewId ? findView(activeViewId, record) : undefined;
  const isDirty = activeView !== undefined && activeView.searchParams !== currentCanonical;

  const filterSummary = useMemo(
    () => summarizeFilters(searchParams, { availableGroups, availableRacks }),
    [searchParams, availableGroups, availableRacks],
  );
  const sortSummary = useMemo(() => summarizeSort(searchParams), [searchParams]);

  const navigateToView = useCallback(
    (view: SavedView) => {
      // All miners is the implicit default state — keep URLs clean.
      if (view.id === ALL_MINERS_VIEW_ID) {
        navigate({ search: "" }, { replace: true });
        return;
      }
      navigate(`?${buildUrlForView(view, searchParams)}`, { replace: true });
    },
    [navigate, searchParams],
  );

  /**
   * After a save/update, sync the URL to the params we just stored so the
   * view is immediately "clean" — otherwise toggling off Include sort order
   * leaves sort/dir in the URL and the view shows as dirty post-save.
   */
  const navigateToSavedView = useCallback(
    (viewId: string, savedParams: string) => {
      const next = new URLSearchParams(savedParams);
      next.set(VIEW_URL_PARAM, viewId);
      navigate(`?${next.toString()}`, { replace: true });
    },
    [navigate],
  );

  const handleSelect = useCallback(
    (id: string) => {
      const view = findView(id, record);
      if (!view) return;
      if (id === activeViewId && !isDirty) return;
      navigateToView(view);
    },
    [record, activeViewId, isDirty, navigateToView],
  );

  const handleOpenNew = useCallback(() => {
    setModal({
      open: true,
      mode: { kind: "create" },
      defaultName: "",
      currentFilters: filterSummary,
      currentSort: sortSummary,
    });
  }, [filterSummary, sortSummary]);

  const handleOpenRename = useCallback(
    (view: SavedView) => {
      const viewFilters = summarizeFilters(new URLSearchParams(view.searchParams), { availableGroups, availableRacks });
      const viewSort = summarizeSort(new URLSearchParams(view.searchParams));
      // Rename launches the same modal; current = saved → no diff badges,
      // user is just editing the name.
      setModal({
        open: true,
        mode: {
          kind: "update",
          viewId: view.id,
          currentName: view.name,
          savedFilters: viewFilters,
          savedSort: viewSort,
        },
        defaultName: view.name,
        currentFilters: viewFilters,
        currentSort: viewSort,
      });
    },
    [availableGroups, availableRacks],
  );

  const handleOpenDelete = useCallback((view: SavedView) => {
    setDeleteConfirm({ open: true, viewId: view.id, viewName: view.name });
  }, []);

  const handleConfirmDelete = useCallback(() => {
    if (!deleteConfirm.open) return;
    const idToDelete = deleteConfirm.viewId;
    deleteUserView(idToDelete);
    setDeleteConfirm({ open: false });
    if (activeViewId === idToDelete) {
      // Active view just deleted — drop the view param from URL.
      const next = new URLSearchParams(searchParams);
      next.delete(VIEW_URL_PARAM);
      const nextSearch = next.toString();
      navigate({ search: nextSearch ? `?${nextSearch}` : "" }, { replace: true });
    }
  }, [deleteConfirm, deleteUserView, activeViewId, navigate, searchParams]);

  const handleSubmit = useCallback(
    ({ name, includeSort }: { name: string; includeSort: boolean }) => {
      if (modal.open && modal.mode.kind === "update") {
        const targetId = modal.mode.viewId;
        const target = record.views.find((view) => view.id === targetId);
        if (!target) {
          setModal({ open: false });
          return;
        }
        const baseCanonical = targetId === activeViewId ? currentCanonical : target.searchParams;
        const paramsForView = includeSort ? baseCanonical : stripSortFromSearchParams(baseCanonical);
        updateUserViewParams(targetId, paramsForView);
        if (name !== target.name) {
          renameUserView(targetId, name);
        }
        // Only realign the URL when we just edited the *active* view —
        // renames on a non-active view shouldn't navigate.
        if (targetId === activeViewId) {
          navigateToSavedView(targetId, paramsForView);
        }
      } else {
        const paramsForView = includeSort ? currentCanonical : stripSortFromSearchParams(currentCanonical);
        const newView = addUserView({ name, searchParams: paramsForView });
        navigateToSavedView(newView.id, paramsForView);
      }
      setModal({ open: false });
    },
    [
      modal,
      record.views,
      activeViewId,
      addUserView,
      currentCanonical,
      navigateToSavedView,
      renameUserView,
      updateUserViewParams,
    ],
  );

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 5 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const handleDragEnd = useCallback(
    (event: DragEndEvent) => {
      const { active, over } = event;
      if (!over || active.id === over.id) return;
      const activeIndex = userViews.findIndex((view) => view.id === active.id);
      const overIndex = userViews.findIndex((view) => view.id === over.id);
      if (activeIndex === -1 || overIndex === -1) return;
      const next = [...userViews];
      const [moved] = next.splice(activeIndex, 1);
      next.splice(overIndex, 0, moved);
      reorderUserViews(next.map((view) => view.id));
    },
    [userViews, reorderUserViews],
  );

  // Reserve built-in names too — otherwise a user view named "All miners"
  // would collide with the built-in tab and produce duplicate labels.
  const existingNames = useMemo(() => {
    const builtInNames = builtIns.map((view) => view.name);
    if (modal.open && modal.mode.kind === "update") {
      const editingId = modal.mode.viewId;
      const otherUserNames = userViews.filter((view) => view.id !== editingId).map((view) => view.name);
      return [...builtInNames, ...otherUserNames];
    }
    return [...builtInNames, ...userViews.map((view) => view.name)];
  }, [builtIns, userViews, modal]);

  return (
    <>
      <div className={clsx("sticky left-0 z-2 w-full", className)} data-testid="views-bar">
        <div className="overflow-x-auto px-6 pt-4 laptop:px-10">
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragEnd={handleDragEnd}
            modifiers={[restrictToHorizontalAxis]}
          >
            <SortableContext items={userViews.map((view) => view.id)} strategy={horizontalListSortingStrategy}>
              <TabStrip activeId={activeViewId} onSelect={handleSelect} ariaLabel="Saved views">
                {builtIns.map((view) => (
                  <BuiltInTab key={view.id} view={view} isActive={view.id === activeViewId} isDirty={isDirty} />
                ))}
                {userViews.map((view) => (
                  <UserTab
                    key={view.id}
                    view={view}
                    isActive={view.id === activeViewId}
                    isDirty={isDirty}
                    onRename={handleOpenRename}
                    onDelete={handleOpenDelete}
                  />
                ))}
                <TabAction
                  text="New view"
                  onClick={handleOpenNew}
                  testId="views-bar-new-view-button"
                  prefixIcon={<Plus width={iconSizes.xSmall} />}
                />
              </TabStrip>
            </SortableContext>
          </DndContext>
        </div>
      </div>

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
        testId="views-bar-delete-dialog"
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

export default ViewsBar;
