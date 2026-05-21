import { useEffect, useMemo, useState } from "react";

import { useBuildings } from "@/protoFleet/api/buildings";
import { type BuildingWithCounts } from "@/protoFleet/api/generated/buildings/v1/buildings_pb";
import { type Site } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { type SiteFormValues } from "@/protoFleet/api/sites";
import FullScreenTwoPaneModal from "@/protoFleet/components/FullScreenTwoPaneModal";
import { Alert } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
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
  // trip through SiteDetailsModal preserves the textarea contents.
  onNetworkConfigChange: (value: string) => void;
  onDismiss: () => void;
  saving?: boolean;
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
  }, [shouldFetchBuildings, fetchSiteId, listBuildingsBySite]);

  // Buildings render as "no buildings" in the non-fetch branches so the
  // operator never sees a stale list from a previous open. The preview
  // grid uses this derived value directly.
  const displayBuildings: BuildingWithCounts[] | undefined = shouldFetchBuildings ? buildings : [];

  const previewTitle = (site?.name || draft.name || "Untitled site").trim();
  const previewLocation = useMemo(() => {
    const parts = [draft.locationCity, draft.locationState].map((s) => s.trim()).filter(Boolean);
    return parts.length > 0 ? parts.join(", ") : "—";
  }, [draft.locationCity, draft.locationState]);
  const previewCapacity = draft.powerCapacityMw > 0 ? `${draft.powerCapacityMw} MW` : "—";
  const buildingCount = displayBuildings?.length ?? 0;

  const handleSave = async () => {
    // Clear any prior warning callout before the next save attempt resolves
    // so a stale message can't flash alongside a clean response. Doing this
    // in the click handler (not an effect) avoids cascading-render warnings.
    setWarnings([]);
    const result = await onSave();
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
        <div className="flex flex-col gap-6 pr-6">
          <section className="flex flex-col gap-2">
            <Header title="Network config" titleSize="text-heading-100" />
            <p className="text-300 text-text-primary-70">One CIDR or IP per line; max 16 KB.</p>
            <Textarea
              id="manage-site-network-config"
              label="Network config"
              initValue={draft.networkConfig}
              onChange={(v) => onNetworkConfigChange(v)}
              rows={8}
              maxLength={16384}
              testId="manage-site-network-config-input"
            />
          </section>

          <section className="flex flex-col gap-2">
            <Header title="Buildings" titleSize="text-heading-100" />
            <PlaceholderBlock label="Buildings table lands in #262" className="h-32" />
          </section>
        </div>
      }
      secondaryPane={
        <div className="flex flex-col gap-4 p-6">
          <div className="flex items-start justify-between gap-4">
            <div className="flex min-w-0 flex-col gap-0.5">
              <span className="truncate text-emphasis-300">{previewTitle}</span>
              <span className="truncate text-300 text-text-primary-70">{previewLocation}</span>
            </div>
            <div className="flex shrink-0 flex-col items-end gap-0.5">
              <span className="text-emphasis-300">{previewCapacity}</span>
              <span className="text-300 text-text-primary-70">
                {buildingCount} {buildingCount === 1 ? "building" : "buildings"}
              </span>
            </div>
          </div>

          {/* Real BuildingCard component lands in #263. Phase 1a renders FPO
              grey boxes so the layout reads as intended without claiming
              behavior it can't deliver. */}
          <div className="grid grid-cols-2 gap-3 tablet:grid-cols-3" data-testid="manage-site-modal-building-grid">
            {displayBuildings === undefined ? (
              <PlaceholderBlock label="Loading buildings…" className="col-span-full h-24" />
            ) : displayBuildings.length === 0 ? (
              <PlaceholderBlock
                label={mode === "create" ? "No buildings yet" : "No buildings in this site"}
                className="col-span-full h-24"
              />
            ) : (
              displayBuildings.map((b) => (
                <PlaceholderBlock
                  key={(b.building?.id ?? 0n).toString()}
                  label={b.building?.name ?? "(unnamed)"}
                  className="h-24"
                />
              ))
            )}
          </div>
        </div>
      }
    />
  );
};

export default ManageSiteModal;
