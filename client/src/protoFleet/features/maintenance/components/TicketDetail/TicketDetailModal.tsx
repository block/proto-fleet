import { useEffect, useMemo, useState } from "react";
import { getComponentIcon, getComponentIconColor } from "../../componentIcons";
import CompletionForm from "./CompletionForm";
import { ResolutionSectionContent } from "./ResolutionSection";
import { RmaSectionContent } from "./RmaSection";
import TicketComments from "./TicketComments";
import {
  MinerIdentifierType,
  type MinerStateSnapshot,
} from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { TicketStatus } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import { lookupMinerByIdentifier } from "@/protoFleet/api/lookupMinerByIdentifier";
import type { UpdateTicketProps } from "@/protoFleet/api/maintenance";
import { useOpenMinerView } from "@/protoFleet/components/SingleMinerWrapper/useOpenMinerView";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useTicketDetail } from "@/protoFleet/features/maintenance/hooks/useTicketDetail";
import type { PartUsageItem, TicketDetail } from "@/protoFleet/features/maintenance/types";
import { useHasPermission } from "@/protoFleet/store";
import { ArrowLeftCompact, ArrowRight, Fleet, Info } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import Row from "@/shared/components/Row";

interface TicketDetailModalProps {
  ticketId: string;
  onDismiss: () => void;
  onMutationSuccess?: () => void;
  ticketIds?: string[];
}
const statusLabels: Partial<Record<TicketStatus, string>> = {
  [TicketStatus.OPEN]: "Open",
  [TicketStatus.IN_PROGRESS]: "In Progress",
  [TicketStatus.ON_HOLD]: "On Hold",
  [TicketStatus.SENT_TO_VENDOR]: "Sent to Vendor",
};
const enumForStatus = {
  unknown: TicketStatus.UNSPECIFIED,
  open: TicketStatus.OPEN,
  in_progress: TicketStatus.IN_PROGRESS,
  on_hold: TicketStatus.ON_HOLD,
  sent_to_vendor: TicketStatus.SENT_TO_VENDOR,
  completed: TicketStatus.COMPLETED,
} as const;
const allowed = (status: string): TicketStatus[] =>
  status === "sent_to_vendor"
    ? [TicketStatus.IN_PROGRESS, TicketStatus.COMPLETED]
    : status === "completed"
      ? []
      : [
          TicketStatus.OPEN,
          TicketStatus.IN_PROGRESS,
          TicketStatus.ON_HOLD,
          TicketStatus.SENT_TO_VENDOR,
          TicketStatus.COMPLETED,
        ].filter((value) => value !== enumForStatus[status as keyof typeof enumForStatus]);

const toDateInputValue = (date: Date | null) => (date ? date.toISOString().slice(0, 10) : "");

const TicketDetailModal = ({
  ticketId,
  onDismiss,
  onMutationSuccess,
  ticketIds = [ticketId],
}: TicketDetailModalProps) => {
  const canManage = useHasPermission("maintenance:manage");
  const canReadMiners = useHasPermission("miner:read");
  const openMinerView = useOpenMinerView();
  const [currentId, setCurrentId] = useState(ticketId);
  const detail = useTicketDetail(currentId);
  const options = useMaintenanceOptions();
  const [editor, setEditor] = useState<"assign" | "status" | "completion" | "rma" | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [editorVersion, setEditorVersion] = useState<TicketDetail["updatedAtSnapshot"]>(null);
  const [completionParts, setCompletionParts] = useState<PartUsageItem[]>([]);
  const [vendor, setVendor] = useState("");
  const [tracking, setTracking] = useState("");
  const [eta, setEta] = useState("");
  const [minerLookup, setMinerLookup] = useState<{
    identifier: string;
    snapshot: MinerStateSnapshot | null;
    error: string | null;
  }>({ identifier: "", snapshot: null, error: null });
  const index = ticketIds.indexOf(currentId);
  const ticket = detail.data;
  const minerIdentifier = ticket?.category === "miner" ? ticket.minerIdentifier : null;
  const linkedMiner = minerLookup.identifier === minerIdentifier ? minerLookup.snapshot : null;
  const minerLinkError = minerLookup.identifier === minerIdentifier ? minerLookup.error : null;
  useEffect(() => {
    if (!minerIdentifier || !canReadMiners) return;
    let active = true;
    const controller = new AbortController();
    void lookupMinerByIdentifier(minerIdentifier, MinerIdentifierType.UNSPECIFIED, controller.signal).then((result) => {
      if (!active) return;
      if (result.status === "found") {
        setMinerLookup({ identifier: minerIdentifier, snapshot: result.snapshot, error: null });
      } else {
        setMinerLookup({
          identifier: minerIdentifier,
          snapshot: null,
          error: result.status === "notFound" ? "Miner is no longer available." : result.message,
        });
      }
    });
    return () => {
      active = false;
      controller.abort();
    };
  }, [canReadMiners, minerIdentifier]);
  const openCompletionEditor = () => {
    if (!ticket) return;
    setCompletionParts(ticket.partsUsed.map((part) => ({ ...part })));
    setEditorVersion(ticket.updatedAtSnapshot);
    setEditor("completion");
  };
  const openRmaEditor = (version = ticket?.updatedAtSnapshot) => {
    if (!ticket || !version) return;
    setVendor(ticket.rmaVendor ?? "");
    setTracking(ticket.rmaTracking ?? "");
    setEta(toDateInputValue(ticket.rmaEta));
    setEditorVersion(version);
    setEditor("rma");
  };
  const navigateToTicket = (id: string) => {
    setEditor(null);
    setCurrentId(id);
  };
  const submitMutation = async (mutation: () => Promise<boolean>) => {
    if (submitting) return false;
    setSubmitting(true);
    try {
      const updated = await mutation();
      if (updated) onMutationSuccess?.();
      return updated;
    } finally {
      setSubmitting(false);
    }
  };
  const updateTicket = async (input: Omit<UpdateTicketProps, "id" | "expectedUpdatedAt">) => {
    if (!editorVersion) return false;
    const updated = await submitMutation(() => detail.update({ ...input, expectedUpdatedAt: editorVersion }));
    if (!updated) setEditor(null);
    return updated;
  };
  const saveRma = () => {
    void updateTicket({
      status: TicketStatus.SENT_TO_VENDOR,
      rmaVendor: vendor,
      rmaTracking: tracking,
      ...(eta ? { rmaEta: new Date(`${eta}T00:00:00.000Z`) } : { clearRmaEta: true }),
    }).then((updated) => {
      if (updated) setEditor(null);
    });
  };
  const canManageTicket =
    canManage && !!ticket && (!ticket.siteId || options.manageableSites.some((site) => site.id === ticket.siteId));
  const canMutate = canManageTicket && ticket?.status !== "completed";
  const buttons = canMutate
    ? [
        {
          text: "Assign",
          variant: variants.secondary,
          disabled: submitting,
          onClick: () => {
            setEditorVersion(ticket?.updatedAtSnapshot ?? null);
            setEditor(editor === "assign" ? null : "assign");
          },
          dismissModalOnClick: false,
        },
        {
          text: "Update status",
          variant: variants.secondary,
          disabled: submitting,
          onClick: () => {
            setEditorVersion(ticket?.updatedAtSnapshot ?? null);
            setEditor(editor === "status" ? null : "status");
          },
          dismissModalOnClick: false,
        },
      ]
    : [];
  const resolutionLabel = useMemo(() => ticket?.resolution.replaceAll("_", " ") ?? "", [ticket?.resolution]);
  return (
    <Modal
      open
      onDismiss={() => {
        if (!submitting) onDismiss();
      }}
      title={ticket?.ticketNumber ?? "Ticket"}
      size="standard"
      divider
      forceTitleCollapsed
      buttons={[
        {
          text: "Refresh",
          variant: variants.secondary,
          disabled: detail.loading || submitting || editor !== null,
          onClick: () => {
            void detail.refresh();
            void options.refresh();
          },
          dismissModalOnClick: false,
        },
        ...buttons,
      ]}
    >
      {detail.loading && !ticket ? (
        <div role="status" aria-label="Loading ticket" className="flex justify-center py-12">
          <ProgressCircular indeterminate />
        </div>
      ) : detail.error && !ticket ? (
        <div role="alert">{detail.error}</div>
      ) : !ticket ? (
        <div role="alert">Ticket not found</div>
      ) : (
        <div className="flex flex-col gap-6">
          {canMutate && (editor === "assign" || editor === "status") ? (
            <div className="rounded-xl bg-surface-elevated-base py-2 shadow-300">
              {editor === "assign" ? (
                <>
                  {ticket.assigneeUserId ? (
                    <div className="px-4">
                      <Row
                        compact
                        onClick={() => {
                          void updateTicket({ clearAssignee: true });
                          setEditor(null);
                        }}
                      >
                        Unassign
                      </Row>
                    </div>
                  ) : null}
                  {options.assignees.map((item) => (
                    <div className="px-4" key={item.id}>
                      <Row
                        compact
                        onClick={() => {
                          void updateTicket({ assigneeUserId: BigInt(item.id) });
                          setEditor(null);
                        }}
                      >
                        {item.username}
                      </Row>
                    </div>
                  ))}
                </>
              ) : (
                allowed(ticket.status)
                  .filter((value) => value !== TicketStatus.COMPLETED)
                  .map((value) => (
                    <div className="px-4" key={value}>
                      <Row
                        compact
                        onClick={() => {
                          if (!editorVersion) return;
                          if (value === TicketStatus.SENT_TO_VENDOR) openRmaEditor(editorVersion);
                          else {
                            setEditor(null);
                            void updateTicket({ status: value });
                          }
                        }}
                      >
                        {statusLabels[value]}
                      </Row>
                    </div>
                  ))
              )}
            </div>
          ) : null}
          <div className="flex flex-col gap-2">
            <div
              className={`flex h-10 w-10 items-center justify-center rounded-xl ${ticket.urgent ? "bg-intent-critical-10" : "bg-surface-5"}`}
            >
              <div className={getComponentIconColor(ticket.urgent)}>
                {getComponentIcon(ticket.component, ticket.urgent)}
              </div>
            </div>
            <h2 className="text-heading-300">
              {ticket.component}: {ticket.diagnosis}
            </h2>
            <span>{ticket.assigneeName ?? "Unassigned"}</span>
          </div>
          <div className="rounded-xl bg-surface-5 p-4">
            <div className="flex items-center gap-3">
              <Info width="w-5" />
              <div className="flex-1">
                <strong>{ticket.status.replaceAll("_", " ")}</strong>
              </div>
              {canMutate ? (
                <Button
                  text="Complete repair"
                  variant={variants.secondary}
                  size={buttonSizes.compact}
                  disabled={submitting}
                  onClick={openCompletionEditor}
                />
              ) : null}
            </div>
            {canMutate && editor === "completion" ? (
              <CompletionForm
                key={ticket.id}
                isMinerTicket={ticket.category === "miner"}
                siteId={ticket.siteId}
                initialParts={completionParts}
                onCancel={() => {
                  if (!submitting) setEditor(null);
                }}
                onSubmit={async (value) => {
                  const updated = await updateTicket({
                    status: TicketStatus.COMPLETED,
                    ...value,
                  });
                  if (updated) setEditor(null);
                  return updated;
                }}
              />
            ) : null}
          </div>
          {canMutate && editor === "rma" ? (
            <div className="flex flex-col gap-3">
              <RmaSectionContent
                vendor={vendor}
                tracking={tracking}
                eta={eta}
                onVendorChange={setVendor}
                onTrackingChange={setTracking}
                onEtaChange={setEta}
              />
              <Button
                text={ticket.status === "sent_to_vendor" ? "Save RMA details" : "Send to vendor"}
                variant={variants.primary}
                disabled={!vendor.trim()}
                loading={submitting}
                onClick={saveRma}
              />
            </div>
          ) : ticket.status === "sent_to_vendor" ? (
            <div className="flex flex-col gap-2 rounded-xl bg-surface-5 p-4">
              <div className="flex items-center justify-between gap-3">
                <span className="text-emphasis-300 font-medium">RMA Details</span>
                {canMutate ? (
                  <Button
                    text="Edit RMA details"
                    variant={variants.secondary}
                    size={buttonSizes.compact}
                    disabled={submitting}
                    onClick={() => openRmaEditor()}
                  />
                ) : null}
              </div>
              <span>Vendor: {ticket.rmaVendor ?? "—"}</span>
              <span>Tracking #: {ticket.rmaTracking ?? "—"}</span>
              <span>ETA: {ticket.rmaEta?.toLocaleDateString(undefined, { timeZone: "UTC" }) ?? "—"}</span>
            </div>
          ) : null}
          {ticket.category === "miner" && ticket.minerIdentifier && canReadMiners ? (
            <div className="flex flex-col gap-2">
              <button
                type="button"
                className="flex items-center gap-3 rounded-xl bg-surface-5 p-4 disabled:cursor-not-allowed disabled:opacity-50"
                disabled={!linkedMiner || submitting}
                onClick={() => {
                  if (!linkedMiner) return;
                  onDismiss();
                  openMinerView(linkedMiner);
                }}
              >
                <Fleet width="w-5" />
                Miner {ticket.minerIdentifier}
              </button>
              {minerLinkError ? <div role="alert">{minerLinkError}</div> : null}
            </div>
          ) : null}
          {ticket.status === "completed" ? (
            <ResolutionSectionContent
              resolution={resolutionLabel}
              repairLocation={ticket.repairLocation.replaceAll("_", " ")}
              partsUsed={ticket.partsUsed.map((part) => ({ name: part.partName, quantity: part.quantity }))}
              notes={ticket.notes}
            />
          ) : null}
          <TicketComments
            key={currentId}
            ticketId={currentId}
            comments={ticket.comments}
            canManage={canManageTicket}
            error={detail.error}
            onAdd={(text, key) => submitMutation(() => detail.addComment(text, key))}
            onDelete={(id) => submitMutation(() => detail.removeComment(id))}
          />
          <Divider />
          <div className="flex items-center justify-between">
            <span>
              {index + 1} of {ticketIds.length} tickets
            </span>
            <div className="flex gap-3">
              <Button
                size={buttonSizes.compact}
                variant={variants.secondary}
                ariaLabel="Previous ticket"
                prefixIcon={<ArrowLeftCompact />}
                onClick={() => navigateToTicket(ticketIds[index - 1])}
                disabled={submitting || index <= 0}
              />
              <Button
                size={buttonSizes.compact}
                variant={variants.secondary}
                ariaLabel="Next ticket"
                prefixIcon={<ArrowRight />}
                onClick={() => navigateToTicket(ticketIds[index + 1])}
                disabled={submitting || index < 0 || index >= ticketIds.length - 1}
              />
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
};
export default TicketDetailModal;
