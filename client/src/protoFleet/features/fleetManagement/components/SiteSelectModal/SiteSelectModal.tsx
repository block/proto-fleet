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
