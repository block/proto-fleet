import { useCallback, useMemo, useState } from "react";
import AddSilenceModal from "./AddSilenceModal";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useNotificationsStore } from "@/protoFleet/features/notifications/store/notificationsStore";
import type { SilenceWithActive } from "@/protoFleet/features/notifications/types";
import { Edit, Stop } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import List from "@/shared/components/List";
import type { ColConfig, ColTitles, ListAction } from "@/shared/components/List/types";
import { pushToast, STATUSES } from "@/shared/features/toaster";

type SilenceColumns = "target" | "window" | "reason";

const colTitles: ColTitles<SilenceColumns> = {
  target: "Target",
  window: "Window",
  reason: "Reason",
};

const activeCols: SilenceColumns[] = ["target", "window", "reason"];

const formatWindow = (silence: SilenceWithActive): string => {
  const start = new Date(silence.starts_at).toLocaleString();
  const end = silence.ends_at ? new Date(silence.ends_at).toLocaleString() : "—";
  return `${start} → ${end}`;
};

const SilencesSection = () => {
  const silences = useNotificationsStore((s) => s.silences);
  const rules = useNotificationsStore((s) => s.rules);
  const removeSilence = useNotificationsStore((s) => s.removeSilence);

  const [showModal, setShowModal] = useState(false);
  const [editingSilence, setEditingSilence] = useState<SilenceWithActive | null>(null);

  // Active first, then by start time ascending; `active` is precomputed by the store to keep this pure.
  const sortedSilences = useMemo<SilenceWithActive[]>(
    () =>
      silences.slice().sort((a, b) => Number(b.active) - Number(a.active) || a.starts_at.localeCompare(b.starts_at)),
    [silences],
  );

  const ruleNameById = useCallback((id: string) => rules.find((r) => r.id === id)?.name ?? id, [rules]);

  const formatTarget = useCallback(
    (silence: SilenceWithActive): string => {
      if (silence.scope.kind === "rule") return silence.scope.rule_id ? ruleNameById(silence.scope.rule_id) : "—";
      if (silence.scope.kind === "group") return `Group: ${silence.scope.group_id ?? "—"}`;
      if (silence.scope.kind === "site") return `Site: ${silence.scope.site_id ?? "—"}`;
      return `${(silence.scope.device_ids ?? []).length} devices`;
    },
    [ruleNameById],
  );

  const openAdd = () => {
    setEditingSilence(null);
    setShowModal(true);
  };

  const handleEdit = useCallback((silence: SilenceWithActive) => {
    setEditingSilence(silence);
    setShowModal(true);
  }, []);

  const handleLift = useCallback(
    async (silence: SilenceWithActive) => {
      try {
        await removeSilence(silence.id);
        pushToast({ message: "Silence lifted", status: STATUSES.success });
      } catch (error) {
        pushToast({
          message: getErrorMessage(error, "Failed to lift silence"),
          status: STATUSES.error,
        });
      }
    },
    [removeSilence],
  );

  const actions: ListAction<SilenceWithActive>[] = useMemo(
    () => [
      {
        title: "Edit",
        icon: <Edit />,
        actionHandler: handleEdit,
      },
      {
        title: "Lift silence",
        icon: <Stop />,
        variant: "destructive",
        actionHandler: (silence) => {
          void handleLift(silence);
        },
      },
    ],
    [handleEdit, handleLift],
  );

  const colConfig: ColConfig<SilenceWithActive, string, SilenceColumns> = useMemo(
    () => ({
      target: {
        component: (silence) => (
          <span className="flex items-center gap-2">
            <span className="text-emphasis-300 text-text-primary">{formatTarget(silence)}</span>
            {silence.active ? (
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
        component: (silence) => <span className="text-text-primary-50">{formatWindow(silence)}</span>,
        width: "w-80",
        allowWrap: true,
      },
      reason: {
        component: (silence) => <span className="text-text-primary-50">{silence.comment || "No reason given"}</span>,
        width: "w-64",
        allowWrap: true,
      },
    }),
    [formatTarget],
  );

  return (
    <section className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
      <div className="flex items-center justify-between">
        <Header title="Silences" titleSize="text-heading-200" />
        <Button variant={variants.secondary} size={sizes.compact} text="Add silence" onClick={openAdd} />
      </div>
      <p className="text-300 text-text-primary-50">
        Temporary mutes that stop a rule from firing during a maintenance window or planned outage.
      </p>

      <List<SilenceWithActive, string, SilenceColumns>
        items={sortedSilences}
        itemKey="id"
        activeCols={activeCols}
        colTitles={colTitles}
        colConfig={colConfig}
        total={sortedSilences.length}
        itemName={{ singular: "silence", plural: "silences" }}
        noDataElement={
          <div className="py-10 text-center text-text-primary-50">
            No silences right now — click Add silence to mute during planned work.
          </div>
        }
        actions={actions}
      />

      <AddSilenceModal
        open={showModal}
        editingSilence={editingSilence}
        onDismiss={() => {
          setShowModal(false);
          setEditingSilence(null);
        }}
      />
    </section>
  );
};

export default SilencesSection;
