import { useMemo } from "react";

import { type SiteWithCounts } from "@/protoFleet/api/generated/sites/v1/sites_pb";
import { variants } from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";

interface SiteSelectModalProps {
  open: boolean;
  sites: SiteWithCounts[];
  title?: string;
  description?: string;
  onSelect: (siteId: bigint, siteName: string) => void;
  onDismiss: () => void;
}

// Lightweight "pick a site" modal used by the Buildings tab Add CTA until
// BuildingSettingsModal grows a built-in site dropdown (#371). Renders a
// list of clickable rows; selection calls onSelect with the chosen site's
// id + name. Callers should skip mounting this modal when there is only
// one site or when the SitePicker is already pinned to a single site.
const SiteSelectModal = ({
  open,
  sites,
  title = "Choose a site",
  description = "Pick the site this building belongs to.",
  onSelect,
  onDismiss,
}: SiteSelectModalProps) => {
  const orderedSites = useMemo(
    () =>
      [...sites]
        .filter((s) => s.site !== undefined)
        .sort((a, b) => (a.site!.name ?? "").localeCompare(b.site!.name ?? "")),
    [sites],
  );

  return (
    <Modal
      open={open}
      title={title}
      description={description}
      onDismiss={onDismiss}
      testId="site-select-modal"
      buttons={[
        {
          text: "Cancel",
          variant: variants.secondary,
          onClick: onDismiss,
          testId: "site-select-modal-cancel",
        },
      ]}
    >
      <div className="flex flex-col" data-testid="site-select-modal-list">
        {orderedSites.map((entry) => (
          <Row
            key={entry.site!.id.toString()}
            onClick={() => onSelect(entry.site!.id, entry.site!.name)}
            testId={`site-select-modal-row-${entry.site!.id}`}
          >
            <span className="text-emphasis-300">{entry.site!.name}</span>
          </Row>
        ))}
      </div>
    </Modal>
  );
};

export default SiteSelectModal;
