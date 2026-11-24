import { type ReactNode, useEffect, useRef, useState } from "react";
import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";

import { parseReportTime, reportTimeInput } from "../reportFormat";
import { ReportingError } from "./ReportingError";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useFleetStore } from "@/protoFleet/store";
import Checkbox from "@/shared/components/Checkbox";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";

export const ReportingCheck = ({
  label,
  checked,
  onChange,
  disabled,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}) => (
  <label className="flex items-center gap-3">
    <Checkbox checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} />
    {label}
  </label>
);
export const ReportingDate = ({
  id,
  label,
  value,
  onChange,
  timezone = "UTC",
}: {
  id: string;
  label: string;
  value?: Timestamp;
  timezone?: string;
  onChange: (value: Timestamp | undefined) => void;
}) => (
  <Input
    id={id}
    key={timezone}
    label={`${label} (${timezone})`}
    type="datetime-local"
    initValue={reportTimeInput(value, timezone)}
    onChange={(value) => onChange(parseReportTime(value, timezone))}
  />
);

export const ReportingForm = ({
  title,
  confirm = "Save",
  valid = true,
  signal,
  onSubmit,
  onDismiss,
  onConflict,
  children,
}: {
  title: string;
  confirm?: string;
  valid?: boolean;
  signal: AbortSignal;
  onSubmit: (signal: AbortSignal) => Promise<void>;
  onDismiss: () => void;
  onConflict: () => void;
  children: ReactNode;
}) => {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const lifetime = useRef<AbortController | null>(null);
  const submitting = useRef(false);
  useEffect(() => {
    const controller = new AbortController();
    lifetime.current = controller;
    return () => controller.abort();
  }, []);
  const submit = async () => {
    if (!valid || conflict || submitting.current || !lifetime.current) return;
    const requestSignal = AbortSignal.any([signal, lifetime.current.signal]);
    const generation = useFleetStore.getState().auth.sessionGeneration;
    submitting.current = true;
    setBusy(true);
    setError("");
    try {
      await onSubmit(requestSignal);
      if (!requestSignal.aborted && generation === useFleetStore.getState().auth.sessionGeneration) onDismiss();
    } catch (cause) {
      if (!requestSignal.aborted && generation === useFleetStore.getState().auth.sessionGeneration) {
        const code = ConnectError.from(cause).code;
        if (code === Code.Aborted || code === Code.FailedPrecondition) {
          setConflict(true);
          onConflict();
          setError(`${getErrorMessage(cause)} Close this form and review the refreshed data before trying again.`);
        } else {
          setError(getErrorMessage(cause));
          if (code === Code.PermissionDenied) {
            setConflict(true);
            onConflict();
          }
          if (code === Code.Unauthenticated) useFleetStore.getState().auth.logout();
        }
      }
    } finally {
      submitting.current = false;
      if (!requestSignal.aborted) setBusy(false);
    }
  };
  return (
    <Modal
      open
      title={title}
      dialogLabel={title}
      manageFocus
      onDismiss={busy ? undefined : onDismiss}
      buttons={[
        { text: "Cancel", variant: "secondary", disabled: busy, onClick: onDismiss, dismissModalOnClick: false },
        {
          text: confirm,
          variant: "primary",
          loading: busy,
          disabled: !valid || conflict,
          onClick: () => void submit(),
          dismissModalOnClick: false,
        },
      ]}
    >
      <div className="flex flex-col gap-4">
        <fieldset className="flex min-w-0 flex-col gap-4" disabled={busy || conflict}>
          {children}
        </fieldset>
        {error ? <ReportingError error={error} /> : null}
      </div>
    </Modal>
  );
};
