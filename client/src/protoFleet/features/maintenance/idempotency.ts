import { useCallback, useRef } from "react";

const createKey = () => {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
};

export const useTicketOperationKey = () => {
  const operation = useRef({ payload: "", key: "" });
  const keyFor = useCallback((payload: unknown) => {
    const encoded = JSON.stringify(payload, (_key, value) => (typeof value === "bigint" ? value.toString() : value));
    if (operation.current.payload !== encoded) operation.current = { payload: encoded, key: createKey() };
    return operation.current.key;
  }, []);
  const reset = useCallback(() => {
    operation.current = { payload: "", key: "" };
  }, []);
  return { keyFor, reset };
};
