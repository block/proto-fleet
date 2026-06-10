import { useCallback, useState } from "react";

import {
  FleetNodeEnrollmentStatus,
  type FleetNodeSummary,
} from "@/protoFleet/api/generated/fleetnodeadmin/v1/fleetnodeadmin_pb";
import { useFleetNodes } from "@/protoFleet/api/useFleetNodes";
import CopyableValue from "@/protoFleet/features/fleetNodes/components/CopyableValue";
import { Alert } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Modal from "@/shared/components/Modal";
import ProgressCircular from "@/shared/components/ProgressCircular";
import { usePoll } from "@/shared/hooks/usePoll";

interface EnrollFleetNodeModalProps {
  open: boolean;
  onDismiss: () => void;
  onConfirmed: () => void;
}

type Step = "createCode" | "confirmNode" | "showApiKey";

const ENROLL_COMMAND =
  "./server/.fleetnode/fleetnode enroll --server-url=http://localhost:4000 --allow-insecure-transport";

const EnrollFleetNodeModal = ({ open, onDismiss, onConfirmed }: EnrollFleetNodeModalProps) => {
  const { createEnrollmentCode, listFleetNodes, confirmFleetNode } = useFleetNodes();

  const [step, setStep] = useState<Step>("createCode");
  const [code, setCode] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [awaitingNodes, setAwaitingNodes] = useState<FleetNodeSummary[]>([]);
  const [selectedNodeId, setSelectedNodeId] = useState<bigint | null>(null);
  const [error, setError] = useState("");
  const [codePending, setCodePending] = useState(false);
  const [confirmPending, setConfirmPending] = useState(false);

  // Reset to a clean slate whenever the modal opens.
  const [prevOpen, setPrevOpen] = useState(open);
  if (prevOpen !== open) {
    setPrevOpen(open);
    if (open) {
      setStep("createCode");
      setCode("");
      setApiKey("");
      setAwaitingNodes([]);
      setSelectedNodeId(null);
      setError("");
    }
  }

  const generateCode = useCallback(() => {
    setCodePending(true);
    setError("");
    createEnrollmentCode({
      onSuccess: (newCode) => setCode(newCode),
      onError: (message) => setError(message || "Failed to create enrollment code"),
      onFinally: () => setCodePending(false),
    });
  }, [createEnrollmentCode]);

  // While confirming, poll for nodes that have registered and are awaiting
  // operator confirmation.
  const fetchAwaiting = useCallback(
    () =>
      listFleetNodes({
        onSuccess: (nodes) =>
          setAwaitingNodes(nodes.filter((n) => n.enrollmentStatus === FleetNodeEnrollmentStatus.AWAITING_CONFIRMATION)),
      }),
    [listFleetNodes],
  );
  usePoll({ fetchData: fetchAwaiting, poll: true, pollIntervalMs: 3000, enabled: open && step === "confirmNode" });

  const handleConfirm = useCallback(() => {
    if (selectedNodeId === null) return;
    setConfirmPending(true);
    setError("");
    confirmFleetNode({
      fleetNodeId: selectedNodeId,
      onSuccess: (key) => {
        setApiKey(key);
        setStep("showApiKey");
      },
      onError: (message) => setError(message || "Failed to confirm fleet node"),
      onFinally: () => setConfirmPending(false),
    });
  }, [selectedNodeId, confirmFleetNode]);

  const handleDone = useCallback(() => {
    onConfirmed();
    onDismiss();
  }, [onConfirmed, onDismiss]);

  const errorCallout = error ? <Callout className="mb-6" intent="danger" prefixIcon={<Alert />} title={error} /> : null;

  if (step === "createCode") {
    return (
      <Modal
        open={open}
        onDismiss={onDismiss}
        title="Enroll fleet node"
        buttons={[
          {
            text: "Next: confirm node",
            onClick: () => setStep("confirmNode"),
            variant: variants.primary,
            disabled: !code,
            dismissModalOnClick: false,
          },
        ]}
        divider={false}
      >
        <div className="mb-4 text-300 text-text-primary-70">
          Generate a one-time enrollment code, then run the fleet node agent and paste the code when prompted:
        </div>
        <div className="mb-4">
          <CopyableValue value={ENROLL_COMMAND} ariaLabel="Copy enroll command" />
        </div>
        {errorCallout}
        <div className="mb-2 text-200 text-text-primary-50">Enrollment code</div>
        {code ? (
          <CopyableValue value={code} ariaLabel="Copy enrollment code" />
        ) : (
          <Button
            variant={variants.secondary}
            size={sizes.compact}
            text="Generate code"
            onClick={generateCode}
            loading={codePending}
            testId="generate-enrollment-code"
          />
        )}
      </Modal>
    );
  }

  if (step === "confirmNode") {
    return (
      <Modal
        open={open}
        onDismiss={onDismiss}
        title="Confirm fleet node"
        buttons={[
          {
            text: "Confirm",
            onClick: handleConfirm,
            variant: variants.primary,
            loading: confirmPending,
            disabled: selectedNodeId === null,
            dismissModalOnClick: false,
          },
        ]}
        divider={false}
      >
        <div className="mb-4 text-300 text-text-primary-70">
          Once the agent registers, it appears below awaiting confirmation. Verify the identity fingerprint matches what
          the agent printed, then confirm.
        </div>
        {errorCallout}
        {awaitingNodes.length === 0 ? (
          <div className="flex items-center gap-2 text-300 text-text-primary-70">
            <ProgressCircular size={16} indeterminate /> Waiting for the agent to register…
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {awaitingNodes.map((node) => {
              const selected = selectedNodeId === node.fleetNodeId;
              return (
                <button
                  key={node.fleetNodeId.toString()}
                  type="button"
                  onClick={() => setSelectedNodeId(node.fleetNodeId)}
                  className={`flex flex-col items-start gap-1 rounded-xl border px-4 py-3 text-left ${
                    selected ? "border-core-primary bg-core-primary-5" : "border-border-5"
                  }`}
                >
                  <span className="text-300 text-text-primary">{node.name}</span>
                  <span className="font-mono text-200 text-text-primary-50">{node.identityFingerprint}</span>
                </button>
              );
            })}
          </div>
        )}
      </Modal>
    );
  }

  return (
    <Modal
      open={open}
      onDismiss={handleDone}
      title="Fleet node confirmed"
      buttons={[{ text: "Done", onClick: handleDone, variant: variants.primary, dismissModalOnClick: false }]}
      divider={false}
    >
      <div className="mb-4 text-300 text-text-primary-70">
        Paste this API key into the agent prompt to finish enrollment. It won&apos;t be shown again.
      </div>
      <CopyableValue value={apiKey} ariaLabel="Copy API key" />
    </Modal>
  );
};

export default EnrollFleetNodeModal;
