import { useCallback, useEffect, useRef, useState } from "react";
import { activityClient } from "@/protoFleet/api/clients";
import type { EventTypeOption, UserOption } from "@/protoFleet/api/generated/activity/v1/activity_pb";
import { useAuthErrors } from "@/protoFleet/store";

interface UseActivityFilterOptionsResult {
  eventTypes: EventTypeOption[];
  scopeTypes: string[];
  users: UserOption[];
  isLoading: boolean;
  error: string | null;
}

interface UseActivityFilterOptionsParams {
  // Off for viewers the server would deny (no activity:read); the options then stay empty.
  enabled?: boolean;
}

// Stable identity so a disabled hook doesn't hand consumers a fresh array every render.
const EMPTY_OPTIONS: never[] = [];

export function useActivityFilterOptions({
  enabled = true,
}: UseActivityFilterOptionsParams = {}): UseActivityFilterOptionsResult {
  const { handleAuthErrors } = useAuthErrors();

  const [eventTypes, setEventTypes] = useState<EventTypeOption[]>([]);
  const [scopeTypes, setScopeTypes] = useState<string[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Bumped on disable and on every new fetch, so a late response cannot repopulate the options.
  const requestIdRef = useRef(0);

  const fetchFilterOptions = useCallback(async () => {
    const requestId = ++requestIdRef.current;
    setIsLoading(true);
    setError(null);
    try {
      const response = await activityClient.listActivityFilterOptions({});
      if (requestId !== requestIdRef.current) return;
      setEventTypes(response.eventTypes);
      setScopeTypes(response.scopeTypes);
      setUsers(response.users);
    } catch (error) {
      if (requestId !== requestIdRef.current) return;
      handleAuthErrors({
        error,
        onError: (err) => {
          const message = err instanceof Error ? err.message : String(err);
          setError(message);
        },
      });
    } finally {
      if (requestId === requestIdRef.current) {
        setIsLoading(false);
      }
    }
  }, [handleAuthErrors]);

  useEffect(() => {
    if (!enabled) {
      // Invalidate any in-flight request so its late response cannot repopulate the options.
      requestIdRef.current++;
      return;
    }
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    void fetchFilterOptions();
  }, [enabled, fetchFilterOptions]);

  // Options fetched under a permission the viewer no longer holds must not stay visible, and an
  // invalidated request's finally cannot settle isLoading, so a disabled hook reports empties itself.
  return {
    eventTypes: enabled ? eventTypes : EMPTY_OPTIONS,
    scopeTypes: enabled ? scopeTypes : EMPTY_OPTIONS,
    users: enabled ? users : EMPTY_OPTIONS,
    isLoading: enabled && isLoading,
    error: enabled ? error : null,
  };
}
