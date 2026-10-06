import { useCallback, useMemo, useState } from "react";
import AddMaintenanceWindowModal from "./AddMaintenanceWindowModal";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useAlertsContext } from "@/protoFleet/features/alerts/api/AlertsContext";
import { isMaintenanceWindowActive } from "@/protoFleet/features/alerts/api/useAlerts";
import { countLabel } from "@/protoFleet/features/alerts/lib/alertCountLabels";
import { useNow } from "@/protoFleet/features/alerts/lib/useNow";
import type { MaintenanceWindowWithActive } from "@/protoFleet/features/alerts/types";
import { useHasPermission } from "@/protoFleet/store";
import { Edit, Stop } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type MaintenanceWindowColumns = "alerts" | "channels" | "window" | "reason";

const colTitles: ColTitles<MaintenanceWindowColumns> = {
  alerts: "Alerts",
  channels: "Destinations",
  window: "Period",
  reason: "Reason",
};

const activeCols: MaintenanceWindowColumns[] = ["alerts", "channels", "window", "reason"];

const periodDateTimeOptions: Intl.DateTimeFormatOptions = {
  year: "numeric",
  month: "numeric",
  day: "numeric",
  hour: "numeric",
  minute: "2-digit",
};

const formatWindow = (maintenanceWindow: MaintenanceWindowWithActive): string => {
  const start = new Date(maintenanceWindow.starts_at).toLocaleString(undefined, periodDateTimeOptions);
  const end = maintenanceWindow.ends_at
    ? new Date(maintenanceWindow.ends_at).toLocaleString(undefined, periodDateTimeOptions)
    : "—";
  return `${start} → ${end}`;
};

// Channel names live in a separate lazily-loaded list; a count keeps this column dependency-free.
const formatChannels = (maintenanceWindow: MaintenanceWindowWithActive): string =>
  maintenanceWindow.channel_ids.length === 0
    ? "All destinations"
    : countLabel(maintenanceWindow.channel_ids.length, "destination");

const MaintenanceWindowsSection = () => {
  const { maintenanceWindows, rules, removeMaintenanceWindow } = useAlertsContext();
  const canManage = useHasPermission("alert:manage");

  const [showModal, setShowModal] = useState(false);
  const [editingMaintenanceWindow, setEditingMaintenanceWindow] = useState<MaintenanceWindowWithActive | null>(null);

  // Recompute active against a ticking clock so the badge/sort flip at the window's
  // start/end boundary instead of freezing at the value computed when it was loaded.
  const now = useNow();
  const sortedMaintenanceWindows = useMemo<MaintenanceWindowWithActive[]>(() => {
    // Order active first, then upcoming/scheduled, then expired, so a future window
    // isn't buried among old history and mistaken for one that was never created.
    const rank = (w: MaintenanceWindowWithActive) => (w.active ? 0 : new Date(w.starts_at).getTime() > now ? 1 : 2);
    return maintenanceWindows
      .map((w) => ({ ...w, active: isMaintenanceWindowActive(w, now) }))
      .sort((a, b) => rank(a) - rank(b) || a.starts_at.localeCompare(b.starts_at));
  }, [maintenanceWindows, now]);

  const ruleNameById = useCallback((id: string) => rules.find((r) => r.id === id)?.name ?? id, [rules]);

  const formatAlerts = useCallback(
    (maintenanceWindow: MaintenanceWindowWithActive): string =>
      maintenanceWindow.rule_ids.length === 0 ? "All alerts" : maintenanceWindow.rule_ids.map(ruleNameById).join(", "),
    [ruleNameById],
  );

  const openAdd = () => {
    setEditingMaintenanceWindow(null);
    setShowModal(true);
  };

  const handleEdit = useCallback((maintenanceWindow: MaintenanceWindowWithActive) => {
    setEditingMaintenanceWindow(maintenanceWindow);
    setShowModal(true);
  }, []);

  const handleLift = useCallback(
    async (maintenanceWindow: MaintenanceWindowWithActive) => {
      try {
        await removeMaintenanceWindow(maintenanceWindow.id);
        pushToast({ message: "Quiet period lifted", status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to lift quiet period"),
          status: STATUSES.error,
        });
      }
    },
    [removeMaintenanceWindow],
  );

  const actions: ListAction<MaintenanceWindowWithActive>[] = useMemo(
    () => [
      {
        title: "Edit",
        icon: <Edit />,
        actionHandler: handleEdit,
      },
      {
        title: "Lift quiet period",
        icon: <Stop />,
        variant: "destructive",
        actionHandler: (maintenanceWindow) => {
          void handleLift(maintenanceWindow);
        },
      },
    ],
    [handleEdit, handleLift],
  );

  const colConfig: ColConfig<MaintenanceWindowWithActive, string, MaintenanceWindowColumns> = useMemo(
    () => ({
      alerts: {
        component: (maintenanceWindow) => (
          <span className="flex items-center gap-2">
            <span className="text-emphasis-300 text-text-primary">{formatAlerts(maintenanceWindow)}</span>
            {maintenanceWindow.active ? (
              <span className="bg-state-success-fill/10 text-state-success-fill rounded px-2 py-0.5 text-200">
                Active
              </span>
            ) : new Date(maintenanceWindow.starts_at).getTime() > now ? (
              <span className="rounded border border-border-5 bg-surface-5 px-2 py-0.5 text-200 text-text-primary">
                Scheduled
              </span>
            ) : (
              <span className="rounded bg-surface-5 px-2 py-0.5 text-200 text-text-primary-50">Expired</span>
            )}
          </span>
        ),
        width: "w-64",
        allowWrap: true,
      },
      channels: {
        component: (maintenanceWindow) => (
          <span className="text-text-primary-50">{formatChannels(maintenanceWindow)}</span>
        ),
        width: "w-32",
      },
      window: {
        component: (maintenanceWindow) => (
          <span className="text-text-primary-50">{formatWindow(maintenanceWindow)}</span>
        ),
        width: "w-80",
        allowWrap: true,
      },
      reason: {
        component: (maintenanceWindow) => (
          <span className="text-text-primary-50">{maintenanceWindow.comment || "No reason given"}</span>
        ),
        width: "w-64",
        allowWrap: true,
      },
    }),
    [formatAlerts, now],
  );

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <div className="flex items-center justify-between">
        <Header title="Quiet Periods" titleSize="text-heading-200" />
        {canManage ? (
          <Button variant={variants.secondary} size={sizes.compact} text="Add quiet period" onClick={openAdd} />
        ) : null}
      </div>
      <p className="text-300 text-text-primary-50">
        Temporary mutes that silence alert delivery during planned work. Muted alerts still show up in history.
      </p>

      <List<MaintenanceWindowWithActive, string, MaintenanceWindowColumns>
        items={sortedMaintenanceWindows}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        total={sortedMaintenanceWindows.length}
        itemName={{ singular: "quiet period", plural: "quiet periods" }}
        noDataElement={
          <div className="py-10 text-center text-text-primary-50">
            No quiet periods right now — click Add quiet period to mute during planned work.
          </div>
        }
        actions={canManage ? actions : []}
      />

      <AddMaintenanceWindowModal
        open={showModal}
        editingMaintenanceWindow={editingMaintenanceWindow}
        onDismiss={() => {
          setShowModal(false);
          setEditingMaintenanceWindow(null);
        }}
      />
    </section>
  );
};

export default MaintenanceWindowsSection;
