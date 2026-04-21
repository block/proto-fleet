import { useCallback } from "react";
import { useNavigate } from "react-router-dom";

import { authClient } from "@/protoFleet/api/clients";
import { useFleetStore } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

/**
 * Hook for logging out the user.
 * Calls the server to invalidate the session, then clears client-side state.
 */
const useLogoutAction = () => {
  const navigate = useNavigate();

  const logout = useCallback(async () => {
    try {
      // Call server to invalidate session and clear cookie
      await authClient.logout({});
    } catch (err) {
      // Show error to user since server-side session may still be valid
      console.error("Error during server logout:", err);
      pushToast({
        message: "Logout may be incomplete. Your session could not be fully invalidated on the server.",
        status: TOAST_STATUSES.error,
      });
    } finally {
      // Always clear client-side auth state
      useFleetStore.getState().auth.logout();
      navigate("/auth");
    }
  }, [navigate]);

  return logout;
};

export { useLogoutAction };
