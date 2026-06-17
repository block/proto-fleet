import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import TicketComments from "./TicketComments";
import { RmaSectionContent } from "./RmaSection";
import { ResolutionSectionContent } from "./ResolutionSection";
import CompletionForm from "./CompletionForm";
import { mockTickets, REPAIR_TECHNICIANS } from "../../mockData";
import { Alert, Asic, Edit, Racks, Info, Checkmark, Pause } from "@/shared/assets/icons";
import Button, { sizes as buttonSizes, variants } from "@/shared/components/Button";
import Divider from "@/shared/components/Divider";
import Modal from "@/shared/components/Modal";

interface TicketDetailModalProps {
  ticketId: string;
  onDismiss: () => void;
  ticketIds?: string[];
}

const STATUS_OPTIONS = ["Open", "In Progress", "On Hold", "Sent to Vendor", "Completed"];

const statusKey = (label: string) => label.toLowerCase().replace(/ /g, "_");

const TicketDetailModal = ({ ticketId, onDismiss, ticketIds }: TicketDetailModalProps) => {
  const [currentId, setCurrentId] = useState(ticketId);
  const [showCompletionForm, setShowCompletionForm] = useState(false);
  const [showAssignMenu, setShowAssignMenu] = useState(false);
  const [showStatusMenu, setShowStatusMenu] = useState(false);

  const ticket = useMemo(
    () => mockTickets.find((t) => t.id === currentId),
    [currentId],
  );

  const navIds = ticketIds ?? mockTickets.map((t) => t.id);
  const currentIndex = navIds.indexOf(currentId);
  const hasPrev = currentIndex > 0;
  const hasNext = currentIndex < navIds.length - 1;

  const handlePrev = useCallback(() => {
    if (hasPrev) setCurrentId(navIds[currentIndex - 1]);
  }, [hasPrev, navIds, currentIndex]);

  const handleNext = useCallback(() => {
    if (hasNext) setCurrentId(navIds[currentIndex + 1]);
  }, [hasNext, navIds, currentIndex]);

  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    // Nudge scroll so the sentinel passes the sticky header, collapsing
    // the title into the top bar on open.
    const timer = setTimeout(() => {
      const el = scrollRef.current?.closest<HTMLElement>("[class*='overflow-auto']");
      if (el) el.scrollTop = 1;
    }, 100);
    return () => clearTimeout(timer);
  }, [currentId]);

  if (!ticket) return null;

  const isMinerTicket = ticket.category === "miner";
  const isSentToVendor = ticket.status === "sent_to_vendor";
  const isCompleted = ticket.status === "completed";

  const statusBannerProps = (() => {
    switch (ticket.status) {
      case "open":
        return {
          icon: <Info width="w-5" />,
          intent: "information" as const,
          title: "Open",
          subtitle: ticket.assigneeName ? `Assigned to ${ticket.assigneeName}` : "Awaiting assignment",
          buttonText: ticket.assigneeName ? "Start repair" : "Assign",
        };
      case "in_progress":
        return {
          icon: <Info width="w-5" />,
          intent: "information" as const,
          title: "In Progress",
          subtitle: ticket.assigneeName ? `Assigned to ${ticket.assigneeName}` : "Repair underway",
          buttonText: "Complete repair",
        };
      case "on_hold":
        return {
          icon: <Pause width="w-5" />,
          intent: "warning" as const,
          title: "On Hold",
          subtitle: "Waiting for parts or info",
          buttonText: "Resume",
        };
      case "sent_to_vendor":
        return {
          icon: <Alert width="w-5" />,
          intent: "information" as const,
          title: "Sent to Vendor",
          subtitle: "Awaiting vendor return",
          buttonText: "Mark received",
        };
      case "completed":
        return {
          icon: <Checkmark width="w-5" />,
          intent: "success" as const,
          title: "Completed",
          subtitle: "Repair completed",
          buttonText: undefined,
        };
      default:
        return null;
    }
  })();

  // Rack grid mock — 5 cols × 5 rows, highlight one slot
  const rackCols = 5;
  const rackRows = 5;
  const highlightSlot = 7;

  return (
    <Modal
      open
      onDismiss={onDismiss}
      title={ticket.ticketNumber}
      size="standard"
      divider
      buttons={[
        {
          text: "Assign ▾",
          variant: variants.secondary,
          onClick: () => { setShowAssignMenu((v) => !v); setShowStatusMenu(false); },
          dismissModalOnClick: false,
        },
        {
          text: "Update status ▾",
          variant: variants.secondary,
          onClick: () => { setShowStatusMenu((v) => !v); setShowAssignMenu(false); },
          dismissModalOnClick: false,
        },
      ]}
    >
      {(showAssignMenu || showStatusMenu) && (
        <>
          <div className="fixed inset-0 z-30" onClick={() => { setShowAssignMenu(false); setShowStatusMenu(false); }} />
          <div className="absolute right-6 top-16 z-40 w-48 rounded-2xl bg-surface-elevated-base py-2 shadow-300">
            {showAssignMenu && (
              <>
                {ticket.assigneeName && (
                  <button type="button" className="w-full px-4 py-2 text-left text-emphasis-300 hover:bg-surface-base" onClick={() => setShowAssignMenu(false)}>Unassign</button>
                )}
                {REPAIR_TECHNICIANS.map((name) => (
                  <button key={name} type="button" className={`w-full px-4 py-2 text-left text-emphasis-300 hover:bg-surface-base ${name === ticket.assigneeName ? "font-medium" : ""}`} onClick={() => setShowAssignMenu(false)}>{name}</button>
                ))}
              </>
            )}
            {showStatusMenu && STATUS_OPTIONS.map((label) => (
              <button key={label} type="button" className={`w-full px-4 py-2 text-left text-emphasis-300 hover:bg-surface-base ${statusKey(label) === ticket.status ? "font-medium" : ""}`} onClick={() => setShowStatusMenu(false)}>{label}</button>
            ))}
          </div>
        </>
      )}
      <div ref={scrollRef} className="flex flex-col gap-6">
        {/* Urgent badge */}
        {ticket.urgent && (
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-intent-critical-10">
            <Alert width="w-5" className="text-text-critical" />
          </div>
        )}

        {/* Title */}
        <div className="flex flex-col gap-1">
          <h2 className="text-heading-300 text-text-primary">
            {ticket.component}: {ticket.diagnosis}
          </h2>
          <span className="text-300 text-text-primary-70">
            {ticket.assigneeName ?? "unassigned"}
          </span>
        </div>

        {showCompletionForm && (
          <CompletionForm
            isMinerTicket={isMinerTicket}
            onSubmit={() => setShowCompletionForm(false)}
            onCancel={() => setShowCompletionForm(false)}
          />
        )}

        {/* Card group — status, linked info, rack — all 4px gap */}
        <div className="flex flex-col gap-1">
          {/* Status card */}
          {statusBannerProps && (
            <div className="flex items-center gap-3 rounded-xl bg-surface-5 p-4">
              <div className="shrink-0 text-text-primary-70">{statusBannerProps.icon}</div>
              <div className="flex flex-1 flex-col">
                <span className="text-emphasis-300 font-medium">{statusBannerProps.title}</span>
                <span className="text-200 text-text-primary-70">{statusBannerProps.subtitle}</span>
              </div>
              {statusBannerProps.buttonText && (
                <Button
                  text={statusBannerProps.buttonText}
                  variant={variants.secondary}
                  size={buttonSizes.compact}
                  onClick={() => {
                    if (ticket.status === "in_progress") setShowCompletionForm(true);
                  }}
                />
              )}
            </div>
          )}

          {/* Linked cards row */}
          <div className="grid grid-cols-2 gap-1">
            {isMinerTicket && ticket.minerIdentifier ? (
              <button
                type="button"
                className="flex items-center gap-3 rounded-xl bg-surface-5 p-4 text-left transition-colors hover:bg-surface-10"
              >
                <Asic width="w-5" className="shrink-0 text-text-primary-70" />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 font-medium">Miner {ticket.minerIdentifier}</span>
                  <span className="text-200 text-text-primary-70">809.7 TH/s</span>
                </div>
              </button>
            ) : (
              <div className="flex items-center gap-3 rounded-xl bg-surface-5 p-4">
                <Info width="w-5" className="shrink-0 text-text-primary-70" />
                <div className="flex flex-col">
                  <span className="text-emphasis-300 font-medium">{ticket.component}</span>
                  <span className="text-200 text-text-primary-70">{ticket.buildingName}, {ticket.siteName}</span>
                </div>
              </div>
            )}
            <div className="flex items-center gap-3 rounded-xl bg-surface-5 p-4">
              <Edit width="w-5" className="shrink-0 text-text-primary-70" />
              <div className="flex flex-col">
                <span className="text-emphasis-300 font-medium">
                  {"alertId" in ticket && ticket.alertId ? "From alert" : "Manually created"}
                </span>
                <span className="text-200 text-text-primary-70">{ticket.age}</span>
              </div>
            </div>
          </div>

          {/* Rack visualization (miner tickets only) */}
          {isMinerTicket && ticket.rackLabel && (
          <div className="flex flex-col gap-3 rounded-xl bg-surface-5 p-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <Racks width="w-5" className="shrink-0 text-text-primary-70" />
                <span className="text-emphasis-300 font-medium">{ticket.rackLabel}, Slot {highlightSlot}</span>
              </div>
              <span className="text-300 text-text-primary-70">{ticket.siteName}, {ticket.buildingName}</span>
            </div>
            <div className="flex justify-center py-2">
              <div
                className="grid gap-1"
                style={{ gridTemplateColumns: `repeat(${rackCols}, 1fr)` }}
              >
                {Array.from({ length: rackCols * rackRows }, (_, i) => {
                  const isHighlight = i === highlightSlot - 1;
                  const isOccupied = [0, 1, 3, 4, 5, 6, 8, 9, 10, 11, 13, 14, 15, 16, 18, 19, 20, 21, 23, 24].includes(i);
                  return (
                    <div
                      key={i}
                      className={`h-4 w-4 rounded-sm ${
                        isHighlight
                          ? "bg-color-text-emphasis"
                          : isOccupied
                            ? "bg-core-primary-fill/10"
                            : "bg-transparent"
                      }`}
                    />
                  );
                })}
              </div>
            </div>
          </div>
        )}
        </div>

        {isSentToVendor && (
          <RmaSectionContent
            vendor=""
            tracking=""
            eta=""
            onVendorChange={() => {}}
            onTrackingChange={() => {}}
            onEtaChange={() => {}}
          />
        )}

        {isCompleted && (
          <ResolutionSectionContent
            resolution="Repaired"
            repairLocation="On-rack"
            partsUsed={[]}
            notes=""
          />
        )}

        <TicketComments ticketId={currentId} />

        {/* Footer — sticky bottom within Modal scroll container */}
        <div className="sticky -bottom-6 -mx-6 -mb-6 bg-surface-elevated-base px-6">
          <Divider className="-mx-6 !w-[calc(100%+3rem)]" />
          <div className="flex items-center justify-between py-5">
            <div className="text-emphasis-300">{currentIndex + 1} of {navIds.length} tickets</div>
            <div className="flex items-center gap-2">
              <Button
                className="py-1"
                size={buttonSizes.textOnly}
                variant={variants.textOnly}
                textColor="text-core-accent-fill"
                textOnlyUnderlineOnHover={false}
                onClick={handlePrev}
                disabled={!hasPrev}
              >
                Previous
              </Button>
              <Button
                className="py-1"
                size={buttonSizes.textOnly}
                variant={variants.textOnly}
                textColor="text-core-accent-fill"
                textOnlyUnderlineOnHover={false}
                onClick={handleNext}
                disabled={!hasNext}
              >
                Next
              </Button>
            </div>
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default TicketDetailModal;
