import { useMemo, useState } from "react";
import clsx from "clsx";

import { type ActiveSite, useActiveSite } from "./useActiveSite";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";
import SkeletonBar from "@/shared/components/SkeletonBar";

const ALL_SITES_LABEL = "All sites";
const UNASSIGNED_LABEL = "Unassigned";

interface SitePickerProps {
  // Sites known to the caller. `undefined` indicates "still loading"; `[]`
  // indicates "no sites" and hides the picker entirely.
  sites: SiteWithCounts[] | undefined;
}

// Phase 1: the picker is mounted globally in PageHeader, but only the new
// multi-site routes (/sites, /settings/sites, /buildings/:id) consume the
// selection. Existing pages (/miners, /racks, dashboards) render the picker
// but ignore the value until #202 wires their queries.
const SitePicker = ({ sites }: SitePickerProps) => {
  const [isOpen, setIsOpen] = useState(false);

  const knownSiteIds = useMemo(() => {
    if (!sites) return new Set<string>();
    return new Set(sites.map((s) => (s.site?.id ?? 0n).toString()).filter((id) => id !== "0"));
  }, [sites]);

  const { activeSite, setActiveSite } = useActiveSite({ knownSiteIds });

  // Loading: show a skeleton so the topbar layout doesn't shift when sites arrive.
  if (sites === undefined) {
    return <SkeletonBar className="w-24" />;
  }

  // Zero sites: hide the picker. Master plan J2 spec.
  if (sites.length === 0) {
    return null;
  }

  const orderedSites = [...sites].sort((a, b) => {
    const an = a.site?.name ?? "";
    const bn = b.site?.name ?? "";
    return an.localeCompare(bn);
  });

  const currentLabel = (() => {
    switch (activeSite.kind) {
      case "all":
        return ALL_SITES_LABEL;
      case "unassigned":
        return UNASSIGNED_LABEL;
      case "site": {
        const match = orderedSites.find((s) => (s.site?.id ?? 0n).toString() === activeSite.id);
        return match?.site?.name ?? ALL_SITES_LABEL;
      }
    }
  })();

  const select = (next: ActiveSite) => {
    setActiveSite(next);
    setIsOpen(false);
  };

  const isSelected = (entry: ActiveSite): boolean => {
    if (entry.kind !== activeSite.kind) return false;
    if (entry.kind === "site" && activeSite.kind === "site") {
      return entry.id === activeSite.id;
    }
    return true;
  };

  return (
    <>
      <button
        type="button"
        className="hover:bg-surface-base-hover flex items-center gap-1 rounded-md px-2 py-1 text-300 text-text-primary focus-visible:underline"
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        aria-label="Active site"
        onClick={() => setIsOpen(true)}
        data-testid="site-picker-trigger"
      >
        <span>{currentLabel}</span>
        {/* Smaller, dimmed chevron matches the prototype's compact trigger affordance. */}
        <ChevronDown className={clsx(iconSizes.xSmall, "opacity-70")} />
      </button>
      <Modal
        open={isOpen}
        onDismiss={() => setIsOpen(false)}
        title="Sites"
        divider={false}
        buttons={[
          {
            variant: variants.secondary,
            text: "Manage sites",
            // Routes to /settings/sites; site CRUD modals (#261) attach to
            // that page so this button is the entry point for full
            // management rather than carrying its own actions.
            onClick: () => {
              setIsOpen(false);
              if (typeof window !== "undefined") {
                window.location.assign("/settings/sites");
              }
            },
            testId: "site-picker-manage-sites",
          },
        ]}
        testId="site-picker-modal"
      >
        <div className="flex flex-col" role="radiogroup" aria-label="Active site">
          <SitePickerOption
            label={ALL_SITES_LABEL}
            selected={isSelected({ kind: "all" })}
            onClick={() => select({ kind: "all" })}
            testId="site-picker-option-all"
          />
          {orderedSites.map((s) => {
            const id = (s.site?.id ?? 0n).toString();
            return (
              <SitePickerOption
                key={id}
                label={s.site?.name ?? "(unnamed)"}
                selected={isSelected({ kind: "site", id })}
                onClick={() => select({ kind: "site", id })}
                testId={`site-picker-option-${id}`}
              />
            );
          })}
          <SitePickerOption
            label={UNASSIGNED_LABEL}
            selected={isSelected({ kind: "unassigned" })}
            onClick={() => select({ kind: "unassigned" })}
            testId="site-picker-option-unassigned"
          />
        </div>
      </Modal>
    </>
  );
};

interface SitePickerOptionProps {
  label: string;
  selected: boolean;
  onClick: () => void;
  testId: string;
}

const SitePickerOption = ({ label, selected, onClick, testId }: SitePickerOptionProps) => (
  <button
    type="button"
    role="radio"
    aria-checked={selected}
    onClick={onClick}
    data-testid={testId}
    className="hover:bg-surface-base-hover focus-visible:bg-surface-base-hover flex w-full items-center gap-3 rounded-md px-2 py-2.5 text-left text-300 text-text-primary"
  >
    <Radio selected={selected} />
    <span>{label}</span>
  </button>
);

export default SitePicker;
