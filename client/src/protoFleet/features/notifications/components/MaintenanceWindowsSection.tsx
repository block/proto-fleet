import { useCallback, useMemo, useState } from "react";
import AddMaintenanceWindowModal from "./AddMaintenanceWindowModal";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useNow } from "@/protoFleet/features/notifications/lib/useNow";
import {
  isMaintenanceWindowActive,
  useNotificationsStore,
} from "@/protoFleet/features/notifications/store/notificationsStore";
import type { MaintenanceWindowWithActive } from "@/protoFleet/features/notifications/types";
import { useHasPermission } from "@/protoFleet/store";
import { Edit, Stop } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type MaintenanceWindowColumns = "target" | "window" | "reason";

const colTitles: ColTitles<MaintenanceWindowColumns> = {
  target: "Target",
  window: "Window",
  reason: "Reason",
};

const activeCols: MaintenanceWindowColumns[] = ["target", "window", "reason"];

const formatWindow = (maintenanceWindow: MaintenanceWindowWithActive): string => {
  const start = new Date(maintenanceWindow.starts_at).toLocaleString();
  const end = maintenanceWindow.ends_at ? new Date(maintenanceWindow.ends_at).toLocaleString() : "—";
  return `${start} → ${end}`;
};

const MaintenanceWindowsSection = () => {
  const maintenanceWindows = useNotificationsStore((s) => s.maintenanceWindows);
  const rules = useNotificationsStore((s) => s.rules);
  const removeMaintenanceWindow = useNotificationsStore((s) => s.removeMaintenanceWindow);
  const canManage = useHasPermission("notification:manage");

  const [showModal, setShowModal] = useState(false);
  const [editingMaintenanceWindow, setEditingMaintenanceWindow] = useState<MaintenanceWindowWithActive | null>(null);

  // Recompute active against a ticking clock so the badge/sort flip at the window's
  // start/end boundary instead of freezing at the value computed when it was loaded.
  const now = useNow();
  const sortedMaintenanceWindows = useMemo<MaintenanceWindowWithActive[]>(
    () =>
      maintenanceWindows
        .map((w) => ({ ...w, active: isMaintenanceWindowActive(w, now) }))
        .sort((a, b) => Number(b.active) - Number(a.active) || a.starts_at.localeCompare(b.starts_at)),
    [maintenanceWindows, now],
  );

  const ruleNameById = useCallback((id: string) => rules.find((r) => r.id === id)?.name ?? id, [rules]);

  const formatTarget = useCallback(
    (maintenanceWindow: MaintenanceWindowWithActive): string => {
      if (maintenanceWindow.scope.kind === "rule")
        return maintenanceWindow.scope.rule_id ? ruleNameById(maintenanceWindow.scope.rule_id) : "—";
      if (maintenanceWindow.scope.kind === "group") return `Group: ${maintenanceWindow.scope.group_id ?? "—"}`;
      if (maintenanceWindow.scope.kind === "site") return `Site: ${maintenanceWindow.scope.site_id ?? "—"}`;
      return `${(maintenanceWindow.scope.device_ids ?? []).length} devices`;
    },
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
        pushToast({ message: "Maintenance window lifted", status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to lift maintenance window"),
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
        title: "Lift maintenance window",
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
      target: {
        component: (maintenanceWindow) => (
          <span className="flex items-center gap-2">
            <span className="text-emphasis-300 text-text-primary">{formatTarget(maintenanceWindow)}</span>
            {maintenanceWindow.active ? (
              <span className="bg-state-success-fill/10 text-state-success-fill rounded px-2 py-0.5 text-200">
                Active
              </span>
            ) : (
              <span className="rounded bg-surface-5 px-2 py-0.5 text-200 text-text-primary-50">Expired</span>
            )}
          </span>
        ),
        width: "w-64",
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
    [formatTarget],
  );

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <div className="flex items-center justify-between">
        <Header title="Maintenance Windows" titleSize="text-heading-200" />
        {canManage ? (
          <Button variant={variants.secondary} size={sizes.compact} text="Add maintenance window" onClick={openAdd} />
        ) : null}
      </div>
      <p className="text-300 text-text-primary-50">
        Temporary mutes that stop a rule from firing during a maintenance window or planned outage.
      </p>

      <List<MaintenanceWindowWithActive, string, MaintenanceWindowColumns>
        items={sortedMaintenanceWindows}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        total={sortedMaintenanceWindows.length}
        itemName={{ singular: "maintenance window", plural: "maintenance windows" }}
        noDataElement={
          <div className="py-10 text-center text-text-primary-50">
            No maintenanceWindows right now — click Add maintenanceWindow to mute during planned work.
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
