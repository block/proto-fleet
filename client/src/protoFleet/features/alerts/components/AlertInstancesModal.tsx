import { usePagedAlerts } from "@/protoFleet/features/alerts/api/usePagedAlerts";
import PagedAlertTable from "@/protoFleet/features/alerts/components/PagedAlertTable";
import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import type { AlertInstanceColumns } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import { alertInstanceColTitles } from "@/protoFleet/features/alerts/lib/alertInstanceColumns";
import type { ActiveAlertGroup } from "@/protoFleet/features/alerts/types";
import { variants } from "@/shared/components/Button";
import Modal, { sizes } from "@/shared/components/Modal";

// Every row is currently firing, so the timestamp reads as an ongoing state rather than an arrival.
const instanceColTitles = { ...alertInstanceColTitles, received: "Last Seen Firing" };

// A miner-scoped alert identifies each instance by its device; a device-less one (an unreachable curtailment
// source) is described only by its summary, so it drops the two identity columns rather than show them empty.
const minerCols: AlertInstanceColumns[] = ["device", "mac", "received", "summary"];
const deviceLessCols: AlertInstanceColumns[] = ["received", "summary"];

interface AlertInstancesModalProps {
  group: ActiveAlertGroup;
  onClose: () => void;
}

// The instances currently firing one alert. Paged rather than fetched whole: an outage's affected set is as
// large as the fleet.
const AlertInstancesModal = ({ group, onClose }: AlertInstancesModalProps) => {
  const hasMiners = group.device_count > 0;
  const { items, loading, error, hasMore, loadMore } = usePagedAlerts(
    // The server filters on the stored rule title, which retired-rule display mapping may have rewritten.
    { active_only: true, alert_name: group.stored_alert_name, rule_group: group.rule_group },
    "Failed to load affected miners",
  );

  return (
    <Modal
      open
      size={sizes.large}
      title={group.alert_name}
      description={
        hasMiners
          ? `${countLabel(group.device_count, "miner")} affected`
          : `${countLabel(group.alert_count, "instance")} firing`
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
          activeCols={hasMiners ? minerCols : deviceLessCols}
          colTitles={instanceColTitles}
          noDataElement={
            <div className="py-6 text-center text-text-primary-50">
              {hasMiners ? "No affected miners." : "No firing instances."}
            </div>
          }
        />
      </div>
    </Modal>
  );
};

export default AlertInstancesModal;
