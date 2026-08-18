import { useCallback, useEffect, useState } from "react";
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

export function useActivityFilterOptions({
  enabled = true,
}: UseActivityFilterOptionsParams = {}): UseActivityFilterOptionsResult {
  const { handleAuthErrors } = useAuthErrors();

  const [eventTypes, setEventTypes] = useState<EventTypeOption[]>([]);
  const [scopeTypes, setScopeTypes] = useState<string[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchFilterOptions = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const response = await activityClient.listActivityFilterOptions({});
      setEventTypes(response.eventTypes);
      setScopeTypes(response.scopeTypes);
      setUsers(response.users);
    } catch (error) {
      handleAuthErrors({
        error,
        onError: (err) => {
          const message = err instanceof Error ? err.message : String(err);
          setError(message);
        },
      });
    } finally {
      setIsLoading(false);
    }
  }, [handleAuthErrors]);

  useEffect(() => {
    if (!enabled) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch on mount; setState inside async fetch is the external-sync pattern
    void fetchFilterOptions();
  }, [enabled, fetchFilterOptions]);

  return { eventTypes, scopeTypes, users, isLoading, error };
}
