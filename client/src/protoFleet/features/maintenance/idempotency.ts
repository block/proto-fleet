import { useCallback, useRef } from "react";
import { createOperationID } from "@/protoFleet/utils/crypto";

export const useTicketOperationKey = () => {
  const operation = useRef({ payload: "", key: "" });
  const keyFor = useCallback((payload: unknown) => {
    const encoded = JSON.stringify(payload, (_key, value) => (typeof value === "bigint" ? value.toString() : value));
    if (operation.current.payload !== encoded) operation.current = { payload: encoded, key: createOperationID() };
    return operation.current.key;
  }, []);
  const reset = useCallback(() => {
    operation.current = { payload: "", key: "" };
  }, []);
  return { keyFor, reset };
};
