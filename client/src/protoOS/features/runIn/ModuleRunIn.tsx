import { useRef, useState } from "react";
import { parseDraft, toDraft, toRunInRequest } from "./defaults";
import RunInForm from "./RunInForm";
import { useRunInDefaults } from "./useRunInDefaults";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import Button from "@/shared/components/Button";
import Modal from "@/shared/components/Modal";

const validationErrorMessage = (error: unknown): string | undefined => {
  if (!error || typeof error !== "object") return undefined;
  const response = error as { status?: unknown; error?: { code?: unknown; message?: unknown } };
  // MDK returns gRPC InvalidArgument (3) as HTTP 400 before accepting a task.
  if (response.status !== 400 || response.error?.code !== 3) return undefined;
  return typeof response.error.message === "string" && response.error.message
    ? response.error.message
    : "The run-in settings were rejected. Review the values and try again.";
};

const ModuleRunIn = ({ moduleId, label }: { moduleId: string; label: string }) => {
  const { adapter } = useMinerHosting();
  const { defaults } = useRunInDefaults();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState(() => toDraft(defaults));
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string>();
  const [taskId, setTaskId] = useState<string>();
  const attempt = useRef<{ config: string; taskId: string; uncertain: boolean } | undefined>(undefined);
  const inFlight = useRef(false);

  const start = async () => {
    if (!adapter?.startModuleRunIn || inFlight.current || taskId) return;
    let request;
    try {
      request = toRunInRequest(parseDraft(draft));
    } catch (error) {
      setError((error as Error).message);
      return;
    }
    const config = JSON.stringify(request);
    if (!attempt.current || attempt.current.config !== config) {
      // Direct miners can use HTTP, where randomUUID is unavailable.
      const taskId = Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) =>
        value.toString(16).padStart(2, "0"),
      ).join("");
      attempt.current = { config, taskId, uncertain: false };
    }
    inFlight.current = true;
    setPending(true);
    setError(undefined);
    try {
      const result = await adapter.startModuleRunIn(moduleId, { ...request, taskId: attempt.current.taskId });
      setTaskId(result.taskId ?? attempt.current.taskId);
    } catch (error) {
      const rejection = validationErrorMessage(error);
      if (rejection && !attempt.current.uncertain) {
        attempt.current = undefined;
        setError(rejection);
      } else {
        // A later rejection cannot resolve whether an earlier request was accepted.
        attempt.current.uncertain = true;
        setError("Could not confirm the run-in start. Try again to check the same request.");
      }
    } finally {
      inFlight.current = false;
      setPending(false);
    }
  };

  if (!adapter?.startModuleRunIn) return null;
  return (
    <>
      <div className="flex items-center gap-4">
        <Button
          text={taskId ? "Run-in submitted" : "Start run-in"}
          variant="secondary"
          size="compact"
          onClick={() => {
            if (!attempt.current) setDraft(toDraft(defaults));
            setOpen(true);
          }}
        />
      </div>
      {open ? (
        <Modal
          open
          title={taskId ? "Run-in submitted" : `Start run-in · ${label}`}
          dialogLabel="Module run-in"
          manageFocus
          onDismiss={() => {
            if (!pending) setOpen(false);
          }}
          buttons={
            taskId
              ? [{ text: "Done", variant: "primary" }]
              : [
                  {
                    text: "Start run-in",
                    variant: "primary",
                    loading: pending,
                    dismissModalOnClick: false,
                    onClick: () => {
                      void start();
                    },
                  },
                ]
          }
        >
          {taskId ? (
            <p>Task {taskId}</p>
          ) : (
            <>
              <p className="mb-6 text-text-primary-70">
                Applies to the connected hashboards in {label}. Review the settings before starting.
              </p>
              {error ? (
                <p role="alert" className="mb-4 text-intent-critical-fill">
                  {error}
                </p>
              ) : null}
              <RunInForm draft={draft} disabled={pending || !!attempt.current} onChange={setDraft} />
            </>
          )}
        </Modal>
      ) : null}
    </>
  );
};

export default ModuleRunIn;
