import { useCallback, useEffect, useRef, useState } from "react";
import { buildFleetNodeEnrollCommand, shouldAppendInsecureTransportFlag } from "./enrollNodeCommand";
import { FleetNodeEnrollmentStatus } from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import type { EnrollmentCode, FleetNodeItem } from "@/protoFleet/api/useFleetNodes";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import { Alert, Copy, Success } from "@/shared/assets/icons";
import { variants } from "@/shared/components/Button";
import { groupVariants } from "@/shared/components/ButtonGroup";
import Callout from "@/shared/components/Callout";
import Dialog, { DialogIcon } from "@/shared/components/Dialog";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { pushToast, STATUSES } from "@/shared/features/toaster";
import { formatTimestamp } from "@/shared/utils/formatTimestamp";
import { copyToClipboard } from "@/shared/utils/utility";

// Poll faster while the modal waits for registration.
const REGISTRATION_POLL_INTERVAL_MS = 3000;

interface EnrollNodeModalProps {
  open: boolean;
  resumeNode?: FleetNodeItem | null;
  onDismiss: () => void;
  onUpdated: () => void;
}

type Step = "code" | "confirm" | "apiKey";

const CopyableValue = ({ value, copyLabel }: { value: string; copyLabel: string }) => {
  const handleCopy = useCallback(() => {
    copyToClipboard(value)
      .then(() => pushToast({ message: `${copyLabel} copied to clipboard`, status: STATUSES.success }))
      .catch(() => pushToast({ message: `Failed to copy ${copyLabel.toLowerCase()}`, status: STATUSES.error }));
  }, [value, copyLabel]);

  return (
    <div className="flex items-center justify-between gap-2 rounded-xl bg-core-primary-5 px-4 py-3">
      <div className="font-mono text-300 break-all text-text-primary">{value}</div>
      <button
        onClick={handleCopy}
        className="shrink-0 text-text-primary"
        aria-label={`Copy ${copyLabel.toLowerCase()}`}
      >
        <Copy />
      </button>
    </div>
  );
};

const EnrollNodeModalContent = ({ open, resumeNode, onDismiss, onUpdated }: EnrollNodeModalProps) => {
  const { listFleetNodes, createEnrollmentCode, confirmFleetNode, revokeFleetNode } = useFleetNodes();
  const [step, setStep] = useState<Step>(open && resumeNode ? "confirm" : "code");
  const [enrollmentCode, setEnrollmentCode] = useState<EnrollmentCode | null>(null);
  const [foundNode, setFoundNode] = useState<FleetNodeItem | null>(open && resumeNode ? resumeNode : null);
  const [apiKey, setApiKey] = useState("");
  const [isConfirming, setIsConfirming] = useState(false);
  const [isRejecting, setIsRejecting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");
  const codeRequestedRef = useRef(false);
  const isMountedRef = useRef(true);

  useEffect(() => {
    isMountedRef.current = true;
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  useEffect(() => {
    if (!open || resumeNode || codeRequestedRef.current) return;
    codeRequestedRef.current = true;
    void (async () => {
      try {
        const created = await createEnrollmentCode();
        if (!isMountedRef.current) return;
        setEnrollmentCode(created);
      } catch (error) {
        if (!isMountedRef.current) return;
        setErrorMsg(error instanceof Error ? error.message : "Failed to create an enrollment code.");
      }
    })();
  }, [open, resumeNode, createEnrollmentCode]);

  useEffect(() => {
    if (!open || step !== "code" || !enrollmentCode) return;
    let cancelled = false;
    let timeoutID: ReturnType<typeof setTimeout> | null = null;
    const poll = async (): Promise<boolean> => {
      try {
        const nodes = await listFleetNodes();
        if (cancelled) return false;
        const candidate = nodes.find(
          (node) =>
            node.enrollmentStatus === FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION &&
            node.pendingEnrollmentId === enrollmentCode.pendingEnrollmentId,
        );
        if (candidate) {
          setFoundNode(candidate);
          setStep("confirm");
          onUpdated();
          return true;
        }
      } catch {
        // Retry on the next tick.
      }
      return false;
    };
    const pollAndSchedule = async () => {
      const found = await poll();
      if (!cancelled && !found) {
        timeoutID = setTimeout(() => void pollAndSchedule(), REGISTRATION_POLL_INTERVAL_MS);
      }
    };
    void pollAndSchedule();
    return () => {
      cancelled = true;
      if (timeoutID !== null) {
        clearTimeout(timeoutID);
      }
    };
  }, [open, step, enrollmentCode, listFleetNodes, onUpdated]);

  const isSubmitting = isConfirming || isRejecting;
  const pendingEnrollmentId = foundNode?.pendingEnrollmentId ?? enrollmentCode?.pendingEnrollmentId;

  const handleDismiss = useCallback(() => {
    // Keep the one-time api_key flow from being stranded.
    if (isSubmitting) return;
    onDismiss();
  }, [isSubmitting, onDismiss]);

  const handleConfirm = useCallback(() => {
    if (!foundNode || isSubmitting) return;
    if (!pendingEnrollmentId) {
      setErrorMsg("Enrollment state is stale. Refresh and try again.");
      return;
    }
    setIsConfirming(true);
    setErrorMsg("");
    void (async () => {
      try {
        const key = await confirmFleetNode(foundNode.fleetNodeId, pendingEnrollmentId);
        if (!isMountedRef.current) return;
        setApiKey(key);
        setStep("apiKey");
        onUpdated();
      } catch (error) {
        if (!isMountedRef.current) return;
        setErrorMsg(error instanceof Error ? error.message : "Failed to confirm the node. Please try again.");
      } finally {
        if (isMountedRef.current) {
          setIsConfirming(false);
        }
      }
    })();
  }, [foundNode, pendingEnrollmentId, isSubmitting, confirmFleetNode, onUpdated]);

  const handleReject = useCallback(() => {
    if (!foundNode || isSubmitting) return;
    if (!pendingEnrollmentId) {
      setErrorMsg("Enrollment state is stale. Refresh and try again.");
      return;
    }
    setIsRejecting(true);
    setErrorMsg("");
    void (async () => {
      try {
        await revokeFleetNode(foundNode.fleetNodeId, pendingEnrollmentId);
        if (!isMountedRef.current) return;
        pushToast({
          message: `Rejected "${foundNode.name}". Start over with a new enrollment code.`,
          status: STATUSES.success,
        });
        onUpdated();
        onDismiss();
      } catch (error) {
        if (!isMountedRef.current) return;
        setErrorMsg(error instanceof Error ? error.message : "Failed to reject the node. Please try again.");
      } finally {
        if (isMountedRef.current) {
          setIsRejecting(false);
        }
      }
    })();
  }, [foundNode, pendingEnrollmentId, isSubmitting, revokeFleetNode, onUpdated, onDismiss]);

  const handleDone = useCallback(() => {
    onUpdated();
    onDismiss();
  }, [onUpdated, onDismiss]);

  const fleetNodeEnrollCommand = buildFleetNodeEnrollCommand(window.location);
  const includesInsecureTransportFlag = shouldAppendInsecureTransportFlag(window.location);

  if (step === "code") {
    return (
      <Modal open={open} onDismiss={handleDismiss} title="Enroll a node" divider={false}>
        <div className="mb-6">
          Nodes are hosts that run the <span className="font-mono">fleet-node</span> daemon and manage the miners on
          their network.
        </div>

        {errorMsg ? (
          <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} />
        ) : enrollmentCode ? (
          <div className="flex flex-col gap-4">
            <div>
              <div className="mb-2">1. On the host you want to enroll, run:</div>
              <CopyableValue value={fleetNodeEnrollCommand} copyLabel="Command" />
              {includesInsecureTransportFlag ? (
                <Callout
                  className="mt-3"
                  intent="warning"
                  prefixIcon={<Alert />}
                  title="HTTP LAN enrollment uses insecure transport"
                  subtitle="Only use this copied command on a trusted local network. Configure HTTPS on the Fleet server for production enrollment."
                />
              ) : null}
            </div>
            <div>
              <div className="mb-2">2. Paste this one-time enrollment code when prompted:</div>
              <CopyableValue value={enrollmentCode.code} copyLabel="Enrollment code" />
              {enrollmentCode.expiresAt ? (
                <div className="mt-2 text-200 text-text-primary-50">
                  Single use. Expires {formatTimestamp(Math.floor(enrollmentCode.expiresAt.getTime() / 1000))}.
                </div>
              ) : null}
            </div>
            <div className="flex items-center gap-3 rounded-xl bg-surface-5 px-4 py-3">
              <ProgressCircular indeterminate size={16} />
              <span>Waiting for the node to register…</span>
            </div>
          </div>
        ) : (
          <div className="flex items-center gap-3 py-2">
            <ProgressCircular indeterminate size={16} />
            <span>Creating an enrollment code…</span>
          </div>
        )}
      </Modal>
    );
  }

  if (step === "confirm") {
    return (
      <Modal
        open={open}
        onDismiss={handleDismiss}
        title="Confirm the node"
        divider={false}
        buttons={[
          {
            text: "Reject",
            onClick: handleReject,
            variant: variants.secondaryDanger,
            loading: isRejecting,
          },
          {
            text: "Fingerprint matches — confirm",
            onClick: handleConfirm,
            variant: variants.primary,
            loading: isConfirming,
            dismissModalOnClick: false,
          },
        ]}
      >
        {errorMsg ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={errorMsg} /> : null}

        <div className="mb-6">A node registered and is awaiting confirmation.</div>

        <div className="mb-6 flex flex-col gap-4">
          <div>
            <div className="mb-2 text-200 text-text-primary-50">Node name</div>
            <div className="text-emphasis-300 text-text-primary">{foundNode?.name}</div>
          </div>
          <div>
            <div className="mb-2 text-200 text-text-primary-50">Identity fingerprint</div>
            <div className="rounded-xl bg-core-primary-5 px-4 py-3 font-mono text-heading-200 tracking-wider text-text-primary">
              {foundNode?.identityFingerprint}
            </div>
          </div>
        </div>

        <Callout
          intent="warning"
          prefixIcon={<Alert />}
          title="Check this is really your host"
          subtitle="The CLI printed the same fingerprint after registering. Only confirm if it matches — a different fingerprint means another host used your enrollment code."
        />
      </Modal>
    );
  }

  return (
    // No onDismiss: the one-time api_key must stay visible until Done.
    <Dialog
      open={open}
      title="Node confirmed"
      subtitle={`Paste this API key into the fleet-node prompt on ${foundNode?.name ?? "the host"} to finish enrollment. It won't be shown again.`}
      subtitleSize="text-300"
      icon={
        <DialogIcon intent="success">
          <Success />
        </DialogIcon>
      }
      buttonGroupVariant={groupVariants.rightAligned}
      buttons={[
        {
          text: "Done",
          onClick: handleDone,
          variant: variants.primary,
        },
      ]}
    >
      <CopyableValue value={apiKey} copyLabel="API key" />
    </Dialog>
  );
};

const EnrollNodeModal = (props: EnrollNodeModalProps) => {
  const modalKey = props.open ? `open-${props.resumeNode?.fleetNodeId ?? "new"}` : "closed";
  return <EnrollNodeModalContent key={modalKey} {...props} />;
};

export default EnrollNodeModal;
