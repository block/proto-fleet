import { useCallback, useEffect, useMemo, useState } from "react";

import type { SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { useSites } from "@/protoFleet/api/sites";
import type { SiteFilterFields } from "@/protoFleet/components/PageHeader/SitePicker";
import Checkbox from "@/shared/components/Checkbox";
import Modal from "@/shared/components/Modal";
import ModalSelectAllFooter from "@/shared/components/Modal/ModalSelectAllFooter";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";
import { pushToast, STATUSES } from "@/shared/features/toaster";

interface SiteSelectionModalProps {
  open: boolean;
  selectedSiteIds: string[];
  // Soft default from the topbar SitePicker. A single selected site limits the
  // sites offered to that one site; "all sites" lists every site. Same
  // filter-the-options model as the rack/building/miner pickers. `listSites`
  // takes no server-side site filter (it returns the org's sites), so the
  // narrowing is applied client-side here.
  scope?: SiteFilterFields;
  onDismiss: () => void;
  onSave: (siteIds: string[]) => void;
}

const SiteSelectionModal = ({ open, selectedSiteIds, scope, onDismiss, onSave }: SiteSelectionModalProps) => {
  const { listSites } = useSites();
  const [sites, setSites] = useState<SiteWithCounts[]>([]);
  const [draftSelection, setDraftSelection] = useState<Set<string>>(new Set(selectedSiteIds));
  const [isLoading, setIsLoading] = useState(true);
  const [hasLoadError, setHasLoadError] = useState(false);

  const scopeSiteIds = scope?.siteIds;
  const scopeKey = (scopeSiteIds ?? []).map(String).join(",");

  useEffect(() => {
    void listSites({
      onSuccess: (rows) => {
        // Narrow to the active site when one is selected; otherwise show all.
        const allowed = scopeSiteIds && scopeSiteIds.length > 0 ? new Set(scopeSiteIds.map(String)) : null;
        const visible = allowed ? rows.filter((row) => allowed.has((row.site?.id ?? 0n).toString())) : rows;
        setSites(visible);
        // While scoped we only see the active site, so preserve preselected ids
        // (a cross-site schedule's other-site targets must survive an
        // open-and-Done). Only prune deleted sites under the unscoped list.
        if (allowed) return;
        const validIds = new Set(visible.map((row) => (row.site?.id ?? 0n).toString()));
        setDraftSelection((current) => new Set([...current].filter((siteId) => validIds.has(siteId))));
      },
      onError: (message: string) => {
        setHasLoadError(true);
        pushToast({ message: message || "Failed to load sites", status: STATUSES.error });
      },
      onFinally: () => setIsLoading(false),
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [listSites, scopeKey]);

  const selectableSiteIds = useMemo(
    () => sites.map((site) => (site.site?.id ?? 0n).toString()).filter((id) => id !== "0"),
    [sites],
  );
  const selectedSiteCount = useMemo(
    () => selectableSiteIds.filter((id) => draftSelection.has(id)).length,
    [draftSelection, selectableSiteIds],
  );
  const allSelected = selectableSiteIds.length > 0 && selectedSiteCount === selectableSiteIds.length;
  const showEmptyState = !isLoading && selectableSiteIds.length === 0;

  const toggleSite = useCallback((siteId: string) => {
    setDraftSelection((current) => {
      const next = new Set(current);
      if (next.has(siteId)) {
        next.delete(siteId);
      } else {
        next.add(siteId);
      }
      return next;
    });
  }, []);

  if (!open) {
    return null;
  }

  return (
    <Modal
      open={open}
      onDismiss={onDismiss}
      title={hasLoadError ? "Couldn't load sites" : showEmptyState ? "No sites configured" : "Select sites"}
      divider={false}
      buttons={[
        {
          text: "Done",
          variant: "primary",
          onClick: () => onSave(Array.from(draftSelection)),
          dismissModalOnClick: false,
          disabled: hasLoadError,
        },
      ]}
    >
      {isLoading ? (
        <div className="flex justify-center py-20">
          <ProgressCircular indeterminate />
        </div>
      ) : hasLoadError ? (
        <div className="text-300 text-text-primary-70">Couldn&apos;t load sites. Close this modal and try again.</div>
      ) : showEmptyState ? (
        <div className="text-300 text-text-primary-70">Set up sites to enable site-wide targeting.</div>
      ) : (
        <div className="flex flex-col">
          <Row divider>
            <label className="flex w-full cursor-pointer items-center gap-4">
              <Checkbox
                checked={allSelected}
                partiallyChecked={!allSelected ? selectedSiteCount > 0 : false}
                onChange={() => setDraftSelection(allSelected ? new Set<string>() : new Set(selectableSiteIds))}
              />
              <div className="flex flex-col">
                <span className="text-emphasis-300 text-text-primary">All sites</span>
              </div>
            </label>
          </Row>

          {sites.map((site) => {
            const siteId = (site.site?.id ?? 0n).toString();
            return (
              <Row key={siteId} divider={false} compact>
                <label className="flex w-full cursor-pointer items-center gap-4">
                  <Checkbox checked={draftSelection.has(siteId)} onChange={() => toggleSite(siteId)} />
                  <div className="flex flex-col">
                    <span className="text-emphasis-300 text-text-primary">{site.site?.name}</span>
                  </div>
                </label>
              </Row>
            );
          })}

          <ModalSelectAllFooter
            label={`${selectedSiteCount} ${selectedSiteCount === 1 ? "site" : "sites"} selected`}
            onSelectAll={() => setDraftSelection(new Set(selectableSiteIds))}
            onSelectNone={() => setDraftSelection(new Set())}
          />
        </div>
      )}
    </Modal>
  );
};

export default SiteSelectionModal;
