import { useCallback, useState } from "react";
import { useLogin, usePassword } from "@/protoOS/api";
import { useAccessToken } from "@/protoOS/features/auth/contexts/AuthContext";
import { Authentication } from "@/shared/components/Setup";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationSettings = () => {
  const { changePassword } = usePassword();
  const login = useLogin();
  const navigate = useNavigate();
  const [isSubmitting, setIsSubmitting] = useState(false);

  const { hasAccess } = useAccessToken(true);

  const submit = useCallback(
    (currentPassword: string, newPassword: string) => {
      const handleError = (message?: string) => {
        setIsSubmitting(false);
        pushToast({
          message: message || "Something went wrong, please try again",
          status: TOAST_STATUSES.error,
        });
      };

      // access token takes a while to propagate, allow caller to pass it from login response
      const doChangePassword = (newAccessToken?: string) => {
        changePassword({
          changePasswordRequest: {
            current_password: currentPassword,
            new_password: newPassword,
          },
          accessTokenValue: newAccessToken,
          onSuccess: () => {
            login({
              password: newPassword,
              onSuccess: () => {
                pushToast({
                  message: "Your password has been updated",
                  status: TOAST_STATUSES.success,
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
        doChangePassword(undefined);
      }
    },
    [hasAccess, changePassword, login, navigate],
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
