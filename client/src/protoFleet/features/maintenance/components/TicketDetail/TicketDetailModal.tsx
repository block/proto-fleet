import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { getComponentIcon, getComponentIconColor } from "../../componentIcons";
import CompletionForm from "./CompletionForm";
import { ResolutionSectionContent } from "./ResolutionSection";
import { RmaSectionContent } from "./RmaSection";
import TicketComments from "./TicketComments";
import { TicketStatus } from "@/protoFleet/api/generated/maintenance/v1/maintenance_pb";
import type { UpdateTicketProps } from "@/protoFleet/api/maintenance";
import { useMaintenanceOptions } from "@/protoFleet/features/maintenance/hooks/useMaintenanceOptions";
import { useTicketDetail } from "@/protoFleet/features/maintenance/hooks/useTicketDetail";
import { useHasPermission } from "@/protoFleet/store";
import { ArrowLeftCompact, ArrowRight, Fleet, Info } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Modal from "@/shared/components/Modal";
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
  const navigate = useNavigate();
  const canManage = useHasPermission("maintenance:manage");
  const [currentId, setCurrentId] = useState(ticketId);
  const detail = useTicketDetail(currentId);
  const options = useMaintenanceOptions();
  const [assignOpen, setAssignOpen] = useState(false);
  const [statusOpen, setStatusOpen] = useState(false);
  const [completing, setCompleting] = useState(false);
  const [rma, setRma] = useState(false);
  const [vendor, setVendor] = useState("");
  const [tracking, setTracking] = useState("");
  const [eta, setEta] = useState("");
  const index = ticketIds.indexOf(currentId);
  const ticket = detail.data;
  const navigateToTicket = (id: string) => {
    setAssignOpen(false);
    setStatusOpen(false);
    setCompleting(false);
    setRma(false);
    setVendor("");
    setTracking("");
    setEta("");
    setCurrentId(id);
  };
  const updateTicket = async (input: Omit<UpdateTicketProps, "id">) => {
    const updated = await detail.update(input);
    if (updated) onMutationSuccess?.();
    return updated;
  };
  const buttons =
    canManage && ticket?.status !== "completed"
      ? [
          {
            text: "Assign",
            variant: variants.secondary,
            onClick: () => setAssignOpen((v) => !v),
            dismissModalOnClick: false,
          },
          {
            text: "Update status",
            variant: variants.secondary,
            onClick: () => setStatusOpen((v) => !v),
            dismissModalOnClick: false,
          },
        ]
      : [];
  const resolutionLabel = useMemo(() => ticket?.resolution.replaceAll("_", " ") ?? "", [ticket?.resolution]);
  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={ticket?.ticketNumber ?? "Ticket"}
      size="standard"
      divider
      forceTitleCollapsed
      buttons={buttons}
    >
      {detail.loading && !ticket ? (
        <div role="status">Loading ticket…</div>
      ) : detail.error && !ticket ? (
        <div role="alert">{detail.error}</div>
      ) : !ticket ? (
        <div role="alert">Ticket not found</div>
      ) : (
        <div className="flex flex-col gap-6">
          {assignOpen || statusOpen ? (
            <div className="rounded-xl bg-surface-elevated-base py-2 shadow-300">
              {assignOpen ? (
                <>
                  {ticket.assigneeUserId ? (
                    <div className="px-4">
                      <Row
                        compact
                        onClick={() => {
                          void updateTicket({ clearAssignee: true });
                          setAssignOpen(false);
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
                          setAssignOpen(false);
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
                          if (value === TicketStatus.SENT_TO_VENDOR) setRma(true);
                          else void updateTicket({ status: value });
                          setStatusOpen(false);
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
              {canManage && ticket.status !== "completed" ? (
                <Button
                  text="Complete repair"
                  variant={variants.secondary}
                  size={buttonSizes.compact}
                  onClick={() => setCompleting(true)}
                />
              ) : null}
            </div>
            {completing ? (
              <CompletionForm
                key={ticket.id}
                isMinerTicket={ticket.category === "miner"}
                siteId={ticket.siteId}
                initialParts={ticket.partsUsed}
                onCancel={() => setCompleting(false)}
                onSubmit={async (value) => {
                  const updated = await updateTicket({ status: TicketStatus.COMPLETED, ...value });
                  if (updated) setCompleting(false);
                  return updated;
                }}
              />
            ) : null}
          </div>
          {rma ? (
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
                onClick={() =>
                  void updateTicket({
                    ...(ticket.status === "sent_to_vendor" ? {} : { status: TicketStatus.SENT_TO_VENDOR }),
                    rmaVendor: vendor,
                    rmaTracking: tracking,
                    rmaEta: eta ? new Date(eta) : undefined,
                  }).then((updated) => {
                    if (updated) setRma(false);
                  })
                }
              />
            </div>
          ) : ticket.status === "sent_to_vendor" ? (
            <div className="flex flex-col gap-2 rounded-xl bg-surface-5 p-4">
              <div className="flex items-center justify-between gap-3">
                <span className="text-emphasis-300 font-medium">RMA Details</span>
                {canManage ? (
                  <Button
                    text="Edit RMA details"
                    variant={variants.secondary}
                    size={buttonSizes.compact}
                    onClick={() => {
                      setVendor(ticket.rmaVendor ?? "");
                      setTracking(ticket.rmaTracking ?? "");
                      setEta(toDateInputValue(ticket.rmaEta));
                      setRma(true);
                    }}
                  />
                ) : null}
              </div>
              <span>Vendor: {ticket.rmaVendor ?? "—"}</span>
              <span>Tracking #: {ticket.rmaTracking ?? "—"}</span>
              <span>ETA: {ticket.rmaEta?.toLocaleDateString() ?? "—"}</span>
            </div>
          ) : null}
          {ticket.category === "miner" && ticket.minerIdentifier ? (
            <button
              type="button"
              className="flex items-center gap-3 rounded-xl bg-surface-5 p-4"
              onClick={() => {
                onDismiss();
                navigate(`/miners/${ticket.minerIdentifier}`);
              }}
            >
              <Fleet width="w-5" />
              Miner {ticket.minerIdentifier}
            </button>
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
            canManage={canManage}
            error={detail.error}
            onAdd={detail.addComment}
            onDelete={detail.removeComment}
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
                disabled={index <= 0}
              />
              <Button
                size={buttonSizes.compact}
                variant={variants.secondary}
                ariaLabel="Next ticket"
                prefixIcon={<ArrowRight />}
                onClick={() => navigateToTicket(ticketIds[index + 1])}
                disabled={index < 0 || index >= ticketIds.length - 1}
              />
            </div>
          </div>
        </div>
      )}
    </Modal>
  );
};
export default TicketDetailModal;
