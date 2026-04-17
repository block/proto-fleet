import { useCallback, useState } from "react";
import { useLogin, usePassword } from "@/protoOS/api";
import { useAccessToken, useSetDefaultPasswordActive } from "@/protoOS/store";
import { Authentication } from "@/shared/components/Setup";
import { pushToast, STATUSES as TOAST_STATUSES, updateToast } from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationSettings = () => {
  const { changePassword } = usePassword();
  const login = useLogin();
  const navigate = useNavigate();
  const setDefaultPasswordActive = useSetDefaultPasswordActive();
  const [isSubmitting, setIsSubmitting] = useState(false);

  const { hasAccess } = useAccessToken(true);

  const submit = useCallback(
    (currentPassword: string, newPassword: string) => {
      setIsSubmitting(true);

      const toast = pushToast({
        message: "Updating your password...",
        status: TOAST_STATUSES.loading,
        ttl: false,
      });

      const handleError = (message?: string) => {
        setIsSubmitting(false);
        updateToast(toast, {
          message: message || "Something went wrong, please try again",
          status: TOAST_STATUSES.error,
          ttl: 3000,
        });
      };

      const doChangePassword = () => {
        changePassword({
          changePasswordRequest: {
            current_password: currentPassword,
            new_password: newPassword,
          },
          onSuccess: () => {
            login({
              password: newPassword,
              onSuccess: () => {
                setDefaultPasswordActive(false);
                setIsSubmitting(false);
                updateToast(toast, {
                  message: "Password updated",
                  status: TOAST_STATUSES.success,
                  ttl: 2000,
                });
                navigate("/");
              },
              onError: handleError,
            });
          },
          onError: handleError,
        });
      };

      if (!hasAccess) {
        // change password request requires valid access token, login user first
        login({
          password: currentPassword,
          onSuccess: doChangePassword,
          onError: handleError,
        });
      } else {
        // use existing access token
        doChangePassword();
      }
    },
    [hasAccess, changePassword, login, navigate, setDefaultPasswordActive],
  );

  return (
    <Authentication
      isUpdateMode
      submit={submit}
      isSubmitting={isSubmitting}
      setIsSubmitting={setIsSubmitting}
      headline="Update your admin login"
      description="Your admin login is used to modify performance settings or mining pool configurations for this miner."
      initUsername="admin"
    />
  );
};

export default AuthenticationSettings;
