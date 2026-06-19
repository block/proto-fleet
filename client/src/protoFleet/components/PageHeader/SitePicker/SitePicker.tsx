import { useMemo, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import clsx from "clsx";

import { type ActiveSite, useActiveSite } from "./useActiveSite";
import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { buildKnownSiteIds } from "@/protoFleet/api/sites";
import { scopeCurrentOrDashboardPath } from "@/protoFleet/routing/siteScope";
import { ChevronDown } from "@/shared/assets/icons";
import { iconSizes } from "@/shared/assets/icons/constants";
import Button, { sizes, variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Radio from "@/shared/components/Radio";
import SkeletonBar from "@/shared/components/SkeletonBar";

const ALL_SITES_LABEL = "All sites";
const UNASSIGNED_LABEL = "Unassigned";

interface SitePickerProps {
  // Sites known to the caller. `undefined` indicates "still loading"; `[]`
  // indicates "no sites" — hidden entirely unless `error` is set, in which
  // case the retry affordance is shown.
  sites: SiteWithCounts[] | undefined;
  // Most recent ListSites error message. When non-null with `sites=[]`, the
  // picker renders an inline "Sites unavailable" affordance with a retry
  // button instead of hiding.
  error?: string | null;
  // Caller-supplied retry handler — typically the same function PageHeader
  // uses to do the initial fetch.
  onRetry?: () => void;
}

// Phase 1b (#202): the picker now scopes the buildings, racks, and miner
// tabs in addition to the new multi-site routes (/sites, /buildings/:id).
// History pages (errors, activity, telemetry,
// dashboards) still ignore the selection — they remain org-wide until
// Phase 2 (#194).
const SitePicker = ({ sites, error, onRetry }: SitePickerProps) => {
  const [isOpen, setIsOpen] = useState(false);
  const navigate = useNavigate();
  const location = useLocation();

  const knownSiteIds = useMemo(() => {
    if (sites === undefined) return undefined;
    if (sites.length === 0 && error != null) return undefined;
    return buildKnownSiteIds(sites);
  }, [error, sites]);

  const orderedSites = useMemo(
    () =>
      sites
        ? [...sites].sort((a, b) => {
            const an = a.site?.name ?? "";
            const bn = b.site?.name ?? "";
            return an.localeCompare(bn);
          })
        : [],
    [sites],
  );

  const { activeSite, setActiveSite } = useActiveSite({ knownSiteIds });

  // Loading: show a skeleton so the topbar layout doesn't shift when sites arrive.
  if (sites === undefined) {
    return <SkeletonBar className="w-24" />;
  }

  // Zero sites + error: surface the failure with a retry. ListSites failures
  // shouldn't silently swallow the picker entirely.
  if (sites.length === 0 && error != null) {
    return (
      <div
        className="flex max-w-full min-w-0 items-center gap-2 text-300 text-text-primary-70"
        data-testid="site-picker-error"
      >
        <span className="min-w-0 truncate">Sites unavailable</span>
        {onRetry ? (
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            className="shrink-0"
            text="Retry"
            onClick={onRetry}
            testId="site-picker-retry"
          />
        ) : null}
      </div>
    );
  }

  // Zero sites (no error): hide the picker.
  if (sites.length === 0) {
    return null;
  }

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
      default: {
        // Exhaustiveness guard: a new ActiveSite variant added without updating
        // this switch produces a TypeScript build error here rather than a
        // silent undefined label at runtime.
        const _exhaustive: never = activeSite;
        void _exhaustive;
        return ALL_SITES_LABEL;
      }
    }
  })();

  const select = (next: ActiveSite) => {
    setActiveSite(next);
    setIsOpen(false);
    navigate(scopeCurrentOrDashboardPath(location.pathname, location.search, location.hash, next));
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
        className="hover:bg-surface-base-hover flex max-w-full min-w-0 items-center gap-1 rounded-md px-2 py-1 text-300 text-text-primary focus-visible:underline"
        aria-haspopup="dialog"
        aria-expanded={isOpen}
        aria-label="Active site"
        onClick={() => setIsOpen(true)}
        data-testid="site-picker-trigger"
      >
        <span className="min-w-0 truncate">{currentLabel}</span>
        {/* Smaller, dimmed chevron matches the prototype's compact trigger affordance. */}
        <ChevronDown className={clsx(iconSizes.xSmall, "shrink-0 opacity-70")} />
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
            // Routes to the Fleet Sites tab so this button is the entry point
            // for full site management rather than carrying its own actions.
            onClick: () => {
              setIsOpen(false);
              navigate("/fleet/sites");
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
