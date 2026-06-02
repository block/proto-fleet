import { useEffect, useMemo, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type SiteFormValues } from "@/protoFleet/api/sites";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { formatSiteAddress } from "@/protoFleet/features/sites/formatAddress";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout, { intents } from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import PlaceholderBlock from "@/shared/components/PlaceholderBlock";
import Textarea from "@/shared/components/Textarea";

export type ManageSiteModalMode = "create" | "edit";

interface ManageSiteModalProps {
  open: boolean;
  mode: ManageSiteModalMode;
  draft: SiteFormValues;
  // In edit mode the parent has a Site row to drive the right-pane preview
  // header off; in create mode there is no row yet so the preview uses the
  // draft values directly.
  site?: Site;
  // Persisted at save time. Returns the canonical site + warnings so the
  // modal can refresh the textarea + surface the Callout without owning
  // the network call itself.
  onSave: () => Promise<{
    canonicalNetworkConfig: string;
    warnings: string[];
    closeOnSuccess: boolean;
  } | null>;
  onEditDetails: () => void;
  // Bubbles draft.networkConfig edits back to the parent state so a round-
  // trip through SiteSettingsModal preserves the textarea contents.
  onNetworkConfigChange: (value: string) => void;
  onDismiss: () => void;
  saving?: boolean;
  // Building-table CTAs. Wired by the host page so the building modal stack
  // shares a single useBuildingModals instance across ManageSiteModal and
  // the settings page table.
  onAddBuilding?: () => void;
  onEditBuilding?: (row: BuildingWithCounts) => void;
  // Refresh signal — bumped by the host whenever the building cache changes
  // (post-create / post-delete) so the modal's local list re-fetches without
  // bouncing through unmount/remount.
  buildingsRefreshKey?: number;
}

const ManageSiteModal = ({
  open,
  mode,
  draft,
  site,
  onSave,
  onEditDetails,
  onNetworkConfigChange,
  onDismiss,
  saving = false,
  onAddBuilding,
  onEditBuilding,
  buildingsRefreshKey = 0,
}: ManageSiteModalProps) => {
  const { listBuildingsBySite } = useBuildings();
  const [buildings, setBuildings] = useState<BuildingWithCounts[] | undefined>(undefined);
  const [warnings, setWarnings] = useState<string[]>([]);

  // Only fetch when edit mode has a persisted site; create mode renders an
  // empty-state placeholder until the first Save lands a row. Skipping the
  // effect entirely for the no-fetch branches keeps the setState-in-effect
  // lint clean and avoids triggering a re-render to clear buildings.
  const shouldFetchBuildings = open && mode === "edit" && site !== undefined;
  const fetchSiteId = shouldFetchBuildings ? site.id : undefined;
  useEffect(() => {
    if (!shouldFetchBuildings || fetchSiteId === undefined) return;
    const controller = new AbortController();
    void listBuildingsBySite({
      siteId: fetchSiteId,
      signal: controller.signal,
      onSuccess: setBuildings,
      onError: () => setBuildings([]),
    });
    return () => controller.abort();
  }, [shouldFetchBuildings, fetchSiteId, listBuildingsBySite, buildingsRefreshKey]);

  // Buildings render as "no buildings" in the non-fetch branches so the
  // operator never sees a stale list from a previous open. The preview
  // grid uses this derived value directly.
  const displayBuildings: BuildingWithCounts[] | undefined = shouldFetchBuildings ? buildings : [];

  const previewTitle = (site?.name || draft.name || "Untitled site").trim();
  const previewLocation = useMemo(() => formatSiteAddress(draft) || "—", [draft]);
  const previewCapacity = draft.powerCapacityMw > 0 ? `${draft.powerCapacityMw} MW` : "—";
  const buildingCount = displayBuildings?.length ?? 0;

  const handleSave = async () => {
    const result = await onSave();
    // Refresh warnings only after a resolved save — clearing them before the
    // await would wipe a still-relevant warning if the next request errors.
    if (!result) return;
    setWarnings(result.warnings);
    if (result.canonicalNetworkConfig !== draft.networkConfig) {
      onNetworkConfigChange(result.canonicalNetworkConfig);
    }
    if (result.closeOnSuccess) {
      onDismiss();
    }
  };

  return (
    <FullScreenTwoPaneModal
      open={open}
      title="Manage Site"
      onDismiss={onDismiss}
      isBusy={saving}
      buttons={[
        {
          text: "Edit details",
          variant: variants.secondary,
          onClick: onEditDetails,
          disabled: saving,
          testId: "manage-site-modal-edit-details",
        },
        {
          text: saving ? "Saving…" : "Save",
          variant: variants.primary,
          onClick: handleSave,
          disabled: saving,
          testId: "manage-site-modal-save",
        },
      ]}
      abovePanes={
        warnings.length > 0 ? (
          <div className="mb-4 px-6 laptop:px-10" data-testid="manage-site-modal-warnings">
            <Callout
              intent={intents.warning}
              prefixIcon={<Alert />}
              title="Network config saved with warnings"
              subtitle={warnings.join(" ")}
            />
          </div>
        ) : null
      }
      primaryPane={
        <div className="flex flex-col gap-6 pr-6 pb-6 laptop:pr-10 laptop:pb-10">
          <section className="flex flex-col gap-2">
            <Header title="Network" titleSize="text-heading-100" />
            {/* Textarea (not Input) because the server contract is a
                newline-separated CIDR/IP list — a single-line input would
                silently strip newlines on type and paste. */}
            <Textarea
              id="manage-site-network-config"
              label="Network"
              initValue={draft.networkConfig}
              onChange={(v) => onNetworkConfigChange(v)}
              rows={6}
              maxLength={16384}
              disabled={saving}
              testId="manage-site-network-config-input"
            />
          </section>

          <section className="flex flex-col gap-2" data-testid="manage-site-modal-buildings-section">
            <div className="flex items-center justify-between">
              <Header title="Buildings" titleSize="text-heading-100" />
              <Button
                variant={variants.secondary}
                size={sizes.compact}
                text="Add building"
                onClick={onAddBuilding ?? (() => undefined)}
                disabled={!onAddBuilding || saving || mode === "create"}
                testId="manage-site-modal-add-building"
              />
            </div>
            {mode === "create" ? (
              <div className="rounded-xl border border-dashed border-border-5 p-4 text-300 text-text-primary-50">
                Save the site first to add buildings.
              </div>
            ) : displayBuildings === undefined ? (
              <div className="text-300 text-text-primary-50">Loading…</div>
            ) : displayBuildings.length === 0 ? (
              <div className="text-300 text-text-primary-50">No buildings in this site yet.</div>
            ) : (
              <ul className="flex flex-col" data-testid="manage-site-modal-buildings-list">
                {displayBuildings.map((b) => {
                  const id = (b.building?.id ?? 0n).toString();
                  const name = b.building?.name ?? "(unnamed)";
                  const rackCount = b.rackCount.toString();
                  const clickable = !!onEditBuilding;
                  return (
                    <li
                      key={id}
                      role={clickable ? "button" : undefined}
                      tabIndex={clickable ? 0 : undefined}
                      className={`flex h-12 items-center justify-between gap-3 border-b border-border-5 ${
                        clickable ? "hover:bg-surface-base-hover cursor-pointer" : ""
                      }`}
                      onClick={clickable ? () => onEditBuilding?.(b) : undefined}
                      onKeyDown={
                        clickable
                          ? (e) => {
                              if (e.key === "Enter" || e.key === " ") {
                                e.preventDefault();
                                onEditBuilding?.(b);
                              }
                            }
                          : undefined
                      }
                      data-testid={`manage-site-modal-building-row-${id}`}
                    >
                      <span className="truncate text-emphasis-300">{name}</span>
                      <span className="shrink-0 text-300 text-text-primary-50">{rackCount} racks</span>
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
        </div>
      }
      secondaryPane={
        <div className="flex h-full min-h-0 flex-col">
          {/* Negative ml escapes wrapper laptop:pl-6 → labels land 20px from pane edge. */}
          <div className="flex shrink-0 items-start justify-between gap-4 pt-5 pr-5 pl-5 laptop:-ml-6 laptop:pl-5">
            <span className="min-w-0 truncate text-300 text-text-primary-50">
              {[previewTitle, previewLocation].filter((s) => s && s !== "—").join(", ") || previewTitle}
            </span>
            <span className="shrink-0 truncate text-300 text-text-primary-50">
              {[previewCapacity, `${buildingCount} ${buildingCount === 1 ? "building" : "buildings"}`]
                .filter((s) => s && s !== "—")
                .join(", ")}
            </span>
          </div>

          {/* Center the FPO building tiles both axes inside the remaining
              space so the preview reads as a centered floor plan. Real
              BuildingCard component lands in #263. */}
          <div className="flex flex-1 items-center justify-center p-5">
            <div className="flex flex-wrap justify-center gap-3" data-testid="manage-site-modal-building-grid">
              {displayBuildings === undefined ? (
                <PlaceholderBlock label="Loading buildings…" className="h-20 w-[120px]" />
              ) : displayBuildings.length === 0 ? (
                <PlaceholderBlock
                  label={mode === "create" ? "No buildings yet" : "No buildings in this site"}
                  className="h-20 w-[120px]"
                />
              ) : (
                displayBuildings.map((b) => (
                  <PlaceholderBlock
                    key={(b.building?.id ?? 0n).toString()}
                    label={b.building?.name ?? "(unnamed)"}
                    className="h-20 w-[120px]"
                  />
                ))
              )}
            </div>
          </div>
        </div>
      }
    />
  );
};

export default ManageSiteModal;
