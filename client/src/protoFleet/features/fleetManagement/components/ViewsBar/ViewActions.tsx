import { useCallback, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import ViewModal, { type ViewModalState } from "./ViewModal";
import type { DeviceSet } from "@/protoFleet/api/generated/device_set/v1/device_set_pb";
import {
  ALL_MINERS_VIEW_ID,
  buildUrlForView,
  canonicalizeSearchParams,
  findView,
  VIEW_URL_PARAM,
  visibleBuiltInViews,
} from "@/protoFleet/features/fleetManagement/views/savedViews";
import type { UseMinerViewsResult } from "@/protoFleet/features/fleetManagement/views/useMinerViews";
import {
  stripSortFromSearchParams,
  summarizeFilters,
  summarizeSort,
} from "@/protoFleet/features/fleetManagement/views/viewSummary";
import { Checkmark, Reboot } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";

type ViewActionsProps = {
  viewsState: UseMinerViewsResult;
  availableGroups: DeviceSet[];
  availableRacks: DeviceSet[];
};

/**
 * Reset / Update view actions for a dirtied view. Mounts in the filter row's
 * right-aligned chip-row slot so it stays in view even when the tab strip
 * overflows or the page narrows.
 */
const ViewActions = ({ viewsState, availableGroups, availableRacks }: ViewActionsProps) => {
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const { record, updateUserViewParams, renameUserView } = viewsState;

  const builtIns = useMemo(() => visibleBuiltInViews(record), [record]);
  const currentCanonical = useMemo(() => canonicalizeSearchParams(searchParams), [searchParams]);

  const activeViewId = searchParams.get(VIEW_URL_PARAM) ?? (currentCanonical === "" ? ALL_MINERS_VIEW_ID : undefined);
  const activeView = activeViewId ? findView(activeViewId, record) : undefined;
  const isModified = activeView !== undefined && activeView.searchParams !== currentCanonical;
  const activeIsBuiltIn = activeView !== undefined && builtIns.some((view) => view.id === activeView.id);

  const currentFilters = useMemo(
    () => summarizeFilters(searchParams, { availableGroups, availableRacks }),
    [searchParams, availableGroups, availableRacks],
  );
  const currentSort = useMemo(() => summarizeSort(searchParams), [searchParams]);

  const savedFilters = useMemo(() => {
    if (!activeView) return [];
    return summarizeFilters(new URLSearchParams(activeView.searchParams), { availableGroups, availableRacks });
  }, [activeView, availableGroups, availableRacks]);
  const savedSort = useMemo(() => {
    if (!activeView) return undefined;
    return summarizeSort(new URLSearchParams(activeView.searchParams));
  }, [activeView]);

  const [modal, setModal] = useState<ViewModalState>({ open: false });

  const handleReset = useCallback(() => {
    if (!activeView) return;
    if (activeView.id === ALL_MINERS_VIEW_ID) {
      navigate({ search: "" }, { replace: true });
      return;
    }
    navigate(`?${buildUrlForView(activeView, searchParams)}`, { replace: true });
  }, [activeView, navigate, searchParams]);

  const handleOpenUpdate = useCallback(() => {
    if (!activeView || activeIsBuiltIn) return;
    setModal({
      open: true,
      mode: {
        kind: "update",
        viewId: activeView.id,
        currentName: activeView.name,
        savedFilters,
        savedSort,
      },
      defaultName: activeView.name,
      currentFilters,
      currentSort,
    });
  }, [activeView, activeIsBuiltIn, savedFilters, savedSort, currentFilters, currentSort]);

  const handleSubmit = useCallback(
    ({ name, includeSort }: { name: string; includeSort: boolean }) => {
      if (!activeView) return;
      const paramsForView = includeSort ? currentCanonical : stripSortFromSearchParams(currentCanonical);
      updateUserViewParams(activeView.id, paramsForView);
      if (name !== activeView.name) {
        renameUserView(activeView.id, name);
      }
      // Sync URL to the saved params so the view is immediately clean —
      // otherwise toggling off Include sort order leaves sort/dir in the URL
      // and the view stays "dirty" right after the user saved.
      const next = new URLSearchParams(paramsForView);
      next.set(VIEW_URL_PARAM, activeView.id);
      navigate(`?${next.toString()}`, { replace: true });
      setModal({ open: false });
    },
    [activeView, currentCanonical, updateUserViewParams, renameUserView, navigate],
  );

  // Reserve built-in names too — otherwise an Update view rename could land
  // on "All miners" / "Offline" / "Needs attention" and produce duplicate tabs.
  const existingNames = useMemo(
    () => [
      ...visibleBuiltInViews(record).map((view) => view.name),
      ...record.views.filter((view) => view.id !== activeView?.id).map((view) => view.name),
    ],
    [record, activeView],
  );

  if (!isModified || !activeView) return null;

  return (
    <>
      <Button
        text="Reset view"
        size={sizes.compact}
        variant={variants.secondary}
        prefixIcon={<Reboot width={iconSizes.small} />}
        onClick={handleReset}
        testId="view-actions-reset-button"
      />
      {activeIsBuiltIn ? null : (
        <Button
          text="Update view"
          size={sizes.compact}
          variant={variants.secondary}
          prefixIcon={<Checkmark width={iconSizes.small} />}
          onClick={handleOpenUpdate}
          testId="view-actions-update-button"
        />
      )}

      <ViewModal
        state={modal}
        existingNames={existingNames}
        onDismiss={() => setModal({ open: false })}
        onSubmit={handleSubmit}
      />
    </>
  );
};

export default ViewActions;
