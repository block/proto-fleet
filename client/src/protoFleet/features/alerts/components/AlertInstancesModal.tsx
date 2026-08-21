import { usePagedAlerts } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import PagedAlertTable from "@/protoFleet/features/alerts/components/PagedAlertTable";
import { activeGroupTitle } from "@/protoFleet/features/alerts/lib/activeGroupTitle";
import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import type { AlertInstanceColumns } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import { alertInstanceColTitles } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";

// Every row is currently firing, so the timestamp reads as an ongoing state rather than an arrival.
const instanceColTitles = { ...alertInstanceColTitles, received: "Last Seen Firing" };

const minerCols: AlertInstanceColumns[] = ["device", "mac", "received", "summary"];

// A device-less rule fires per source rather than per miner, so there is no identity column to show: the summary
// is what names the firing instance ("Curtailment source maestro-b is unreachable").
const instanceCols: AlertInstanceColumns[] = ["received", "summary"];

interface AlertInstancesModalProps {
  group: ActiveAlertGroup;
  onClose: () => void;
}

// The instances currently firing one alert. Paged rather than fetched whole: an outage's affected set is as
// large as the fleet.
const AlertInstancesModal = ({ group, onClose }: AlertInstancesModalProps) => {
  const perMiner = group.device_count > 0;
  const { items, loading, error, hasMore, loadMore } = usePagedAlerts(
    // The server filters on the stored rule title, which retired-rule display mapping may have rewritten.
    { active_only: true, alert_name: group.stored_alert_name, rule_group: group.rule_group },
    perMiner ? "Failed to load affected miners" : "Failed to load firing instances",
  );

  return (
    <Modal
      open
      size={sizes.large}
      title={activeGroupTitle(group)}
      description={
        perMiner ? `${countLabel(group.device_count, "miner")} affected` : countLabel(group.alert_count, "instance")
      }
      onDismiss={onClose}
      buttons={[{ text: "Done", variant: variants.primary, onClick: onClose }]}
    >
      <div className="flex flex-col gap-4">
        <PagedAlertTable
          items={items}
          loading={loading}
          error={error}
          hasMore={hasMore}
          onLoadMore={loadMore}
          activeCols={perMiner ? minerCols : instanceCols}
          colTitles={instanceColTitles}
          // The modal is an elevated surface, which is a different grey from the page in dark mode.
          stickyBgColor="bg-surface-elevated-base"
          noDataElement={
            <div className="py-6 text-center text-text-primary-50">
              {perMiner ? "No affected miners." : "No firing instances."}
            </div>
          }
        />
      </div>
    </Modal>
  );
};

export default AlertInstancesModal;
