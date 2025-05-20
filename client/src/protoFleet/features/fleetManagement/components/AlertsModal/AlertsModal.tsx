import { useMemo, useState } from "react";
import { Alert } from "./types";
import alertColConfig from "@/protoFleet/features/fleetManagement/components/AlertsModal/alertColConfig";
import AlertListActionBar from "@/protoFleet/features/fleetManagement/components/AlertsModal/AlertListActionBar";
import ConfirmArchiveDialog from "@/protoFleet/features/fleetManagement/components/AlertsModal/ConfirmArchiveDialog";
import {
  alertCols,
  alertColTitles,
  AlertType,
  alertTypes,
  AlertView,
  alertViews,
} from "@/protoFleet/features/fleetManagement/components/AlertsModal/constants";
import { Notification } from "@/shared/assets/icons";
import { sizes, variants } from "@/shared/components/Button";
import List from "@/shared/components/List";
import { defaultListFilter } from "@/shared/components/List/constants";
import { FilterItem } from "@/shared/components/List/Filters/types";
import Modal from "@/shared/components/Modal";
import SegmentedControl from "@/shared/components/SegmentedControl";
import { useNavigate } from "@/shared/hooks/useNavigate";

interface AlertsModalProps {
  show: boolean;
  alerts: Alert[];
  onDismiss: () => void;
}

// TODO: move this to state when we implement row customization
const activeCols = [
  alertCols.name,
  alertCols.status,
  alertCols.error,
  alertCols.timestamp,
] as (keyof Alert)[];

const AlertsModal = ({ show, alerts, onDismiss }: AlertsModalProps) => {
  const navigate = useNavigate();

  const [selectedView, setSelectedView] = useState<AlertView>(
    alertViews.active,
  );
  const [showArchiveAllDialog, setShowArchiveAllDialog] = useState(false);

  const handleSelect = (selectedKey: string) => {
    setSelectedView(selectedKey);
  };

  const modalButtons = useMemo(() => {
    const handleArchiveAllAlerts = () => {
      setShowArchiveAllDialog(true);
    };

    const archiveButton = {
      text: "Archive all alerts",
      onClick: handleArchiveAllAlerts,
      variant: variants.secondary,
    };
    const doneButton = {
      text: "Done",
      variant: variants.primary,
    };

    return selectedView === alertViews.active
      ? [archiveButton, doneButton]
      : [doneButton];
  }, [selectedView]);

  const handleConfirmArchiveAll = () => {
    // TODO call API
    setShowArchiveAllDialog(false);
  };

  const filters = useMemo(() => {
    const countAlerts = (type: AlertType) => {
      return alerts.filter((alert) => alert.alertType === type).length;
    };

    return [
      {
        type: "button",
        title: "All alerts",
        value: defaultListFilter,
        count: alerts.length,
      },
      {
        type: "button",
        title: "Control board",
        value: alertTypes.controlBoard,
        count: countAlerts(alertTypes.controlBoard),
      },
      {
        type: "button",
        title: "Fan",
        value: alertTypes.fan,
        count: countAlerts(alertTypes.fan),
      },
      {
        type: "button",
        title: "Hashboard",
        value: alertTypes.hashboard,
        count: countAlerts(alertTypes.hashboard),
      },
      {
        type: "button",
        title: "PSU",
        value: alertTypes.psu,
        count: countAlerts(alertTypes.psu),
      },
      {
        type: "button",
        title: "Pool",
        value: alertTypes.pool,
        count: countAlerts(alertTypes.pool),
      },
    ] as FilterItem<AlertType>[];
  }, [alerts]);

  const filterAlert = (
    item: Alert,
    activeButtonFilters: (AlertType | typeof defaultListFilter)[],
  ) => {
    return (
      activeButtonFilters.includes(defaultListFilter) ||
      activeButtonFilters.some((filter) => item.alertType === filter)
    );
  };

  const listActions = useMemo(() => {
    const handleArchive = () => {
      // TODO handle archive action
    };

    const handleUnarchive = () => {
      // TODO handle unarchive action
    };

    const handleViewMiner = (item: Alert) => {
      navigate(`/miners/${item.minerIp}/hashrate`);
    };

    const handleRebootMiner = () => {
      // TODO handle reboot action
    };

    let action = {
      title: "Archive",
      actionHandler: handleArchive,
    };
    if (selectedView === alertViews.archive) {
      action = {
        title: "Unarchive",
        actionHandler: handleUnarchive,
      };
    }

    return [
      action,
      {
        title: "View miner",
        actionHandler: handleViewMiner,
      },
      {
        title: "Reboot miner",
        actionHandler: handleRebootMiner,
      },
    ];
  }, [navigate, selectedView]);

  return (
    <Modal
      className="relative flex max-h-3/4 !w-3xl flex-col phone:!w-[calc(100vw-theme(spacing.4))] tablet:!w-[calc(100vw-theme(spacing.4))]"
      bodyClassName="flex flex-col overflow-y-auto"
      show={show}
      buttons={modalButtons}
      onDismiss={onDismiss}
    >
      <ConfirmArchiveDialog
        show={showArchiveAllDialog}
        onConfirm={handleConfirmArchiveAll}
        onCancel={() => setShowArchiveAllDialog(false)}
      />
      <SegmentedControl
        className="mt-6"
        segments={[
          {
            key: alertViews.active,
            title: "Active",
          },
          {
            key: alertViews.archive,
            title: "Archive",
          },
        ]}
        onSelect={handleSelect}
      />
      <List<Alert, Alert["minerMacAddress"], AlertType>
        activeCols={
          selectedView === alertViews.active
            ? activeCols
            : activeCols.filter((row) => row !== alertCols.status)
        }
        colTitles={alertColTitles}
        colConfig={alertColConfig}
        filters={filters}
        filterItem={filterAlert}
        filterSize={sizes.compact}
        items={alerts}
        itemKey="minerMacAddress"
        actions={listActions}
        itemSelectable={selectedView === alertViews.active}
        disabled={selectedView === alertViews.archive}
        renderActionBar={(selectedItems) => (
          <AlertListActionBar selectedAlerts={selectedItems} />
        )}
        noDataElement={
          <div className="flex h-108 items-center justify-center rounded-2xl bg-core-primary-5">
            <div className="flex flex-col items-center gap-4 text-text-primary-50">
              <Notification />
              <div className="text-heading-200">
                {selectedView === alertViews.active
                  ? "No active alerts"
                  : "No archived alerts"}
              </div>
            </div>
          </div>
        }
      />
    </Modal>
  );
};

export default AlertsModal;
