import { useEffect, useRef, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";

import { useAccountScope } from "./AccountScope";
import { getErrorMessage } from "@/protoFleet/api/getErrorMessage";
import { useFleetStore } from "@/protoFleet/store";

export const useAccountMutation = () => {
  const scope = useAccountScope();
  const submitting = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const run = async (operation: (current: () => boolean) => Promise<void>) => {
    if (submitting.current || scope?.readOnly || scope?.signal.aborted) return false;
    submitting.current = true;
    const generation = useFleetStore.getState().auth.sessionGeneration;
    const current = () =>
      mounted.current && !scope?.signal.aborted && useFleetStore.getState().auth.sessionGeneration === generation;
    setBusy(true);
    setError("");
    try {
      // Multi-request operations must check their initiating lifetime between
      // requests, before dispatching a follow-up mutation or delivering callbacks.
      await operation(current);
      return current();
    } catch (cause) {
      if (!current()) return false;
      setError(getErrorMessage(cause));
      if (ConnectError.from(cause).code === Code.PermissionDenied) scope?.refreshAccount();
      if (ConnectError.from(cause).code === Code.Unauthenticated) useFleetStore.getState().auth.logout();
      return false;
    } finally {
      submitting.current = false;
      if (current()) setBusy(false);
    }
  };
  return { run, busy, error, clearError: () => setError("") };
};
