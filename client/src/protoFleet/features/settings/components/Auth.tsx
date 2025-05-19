import { useState } from "react";
import { useLogin } from "@/protoFleet/api/useLogin";
import { usePassword } from "@/protoFleet/api/usePassword";
import { Authentication } from "@/shared/components/Setup";
import {
  pushToast,
  STATUSES as TOAST_STATUSES,
} from "@/shared/features/toaster";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationSettings = () => {
  const { updatePassword } = usePassword();
  const login = useLogin();
  const navigate = useNavigate();
  const [isSubmitting, setIsSubmitting] = useState(false);

  function submit(currentPassword: string, newPassword: string) {
    updatePassword({
      currentPassword: currentPassword,
      newPassword: newPassword,
      onSuccess: () => {
        login({
          password: newPassword,
          onFinally: () => {
            pushToast({
              message: "Your password has been updated",
              status: TOAST_STATUSES.success,
            });
            navigate("/");
          },
        });
      },
      onError: () => {
        setIsSubmitting(false);
        pushToast({
          message: "Something went wrong, please try again",
          status: TOAST_STATUSES.error,
        });
      },
    });
  }

  return (
    <>
      <div className="mx-auto max-w-xl">
        <Authentication
          isUpdateMode
          submit={submit}
          isSubmitting={isSubmitting}
          setIsSubmitting={setIsSubmitting}
          headline="Update your admin login"
          description="Your admin login is used to manage and make changes to this network’s miners, miner settings, and security configurations."
        />
      </div>
    </>
  );
};

export default AuthenticationSettings;
