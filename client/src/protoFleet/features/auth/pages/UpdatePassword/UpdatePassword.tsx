import { useCallback, useEffect, useState } from "react";
import { useAuth } from "@/protoFleet/api/useAuth";
import { UpdatePasswordForm, UpdatePasswordSuccess } from "@/protoFleet/features/auth/components";
import { useSetTemporaryPassword, useTemporaryPassword } from "@/protoFleet/store";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const UpdatePassword = () => {
  const navigate = useNavigate();
  const { updatePassword } = useAuth();
  const temporaryPassword = useTemporaryPassword();
  const setTemporaryPassword = useSetTemporaryPassword();

  const [errorMsg, setErrorMsg] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isSuccess, setIsSuccess] = useState(false);

  // Redirect to login if no temporary password is available on mount
  useEffect(() => {
    if (!temporaryPassword) {
      pushToast({
        message: "Session expired. Please log in again.",
        status: TOAST_STATUSES.error,
      });
      navigate("/");
    }
    // Only check on initial mount, not when temporaryPassword changes
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Clear temporary password on unmount for security
  useEffect(() => {
    return () => {
      setTemporaryPassword(null);
    };
  }, [setTemporaryPassword]);

  const handleUpdatePassword = useCallback(
    (newPassword: string, _confirmPassword: string) => {
      // Form handles validation (password length, match, weak password warning)
      setIsSubmitting(true);
      setErrorMsg("");

      updatePassword({
        currentPassword: temporaryPassword!,
        newPassword,
        onSuccess: () => {
          setTemporaryPassword(null);
          setIsSuccess(true);
          pushToast({
            message: "Password updated",
            status: TOAST_STATUSES.success,
          });
        },
        onError: (error: string) => {
          setErrorMsg(error || "Failed to update password. Please try again.");
        },
        onFinally: () => {
          setIsSubmitting(false);
        },
      });
    },
    [temporaryPassword, updatePassword, setTemporaryPassword],
  );

  const handleLogin = useCallback(() => {
    navigate("/");
  }, [navigate]);

  if (isSuccess) {
    return <UpdatePasswordSuccess onLogin={handleLogin} />;
  }

  return (
    <UpdatePasswordForm
      onSubmit={handleUpdatePassword}
      isSubmitting={isSubmitting}
      errorMsg={errorMsg}
      onErrorDismiss={() => setErrorMsg("")}
    />
  );
};

export default UpdatePassword;
