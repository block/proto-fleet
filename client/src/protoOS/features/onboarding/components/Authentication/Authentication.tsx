import { useEffect, useState } from "react";
import { useLogin, usePassword } from "@/protoOS/api";
import { useDefaultPasswordActive, usePasswordSet, useSetDefaultPasswordActive } from "@/protoOS/store";
import { Authentication, OnboardingLayout } from "@/shared/components/Setup";
import { useNavigate } from "@/shared/hooks/useNavigate";

const AuthenticationPage = () => {
  const navigate = useNavigate();
  const { changePassword, setPassword } = usePassword();
  const login = useLogin();
  const isPasswordSet = usePasswordSet();
  const isDefaultPasswordActive = useDefaultPasswordActive() ?? false;
  const setDefaultPasswordActive = useSetDefaultPasswordActive();
  const [submitError, setSubmitError] = useState<string | undefined>();
  const [isSubmitting, setIsSubmitting] = useState(false);
  const isChangingDefaultPassword = isPasswordSet === true && isDefaultPasswordActive === true;

  useEffect(() => {
    if (isPasswordSet === true && isDefaultPasswordActive === false) {
      navigate("/onboarding/mining-pool");
    }
  }, [navigate, isPasswordSet, isDefaultPasswordActive]);

  function handleError(message?: string) {
    setSubmitError(message);
    setIsSubmitting(false);
  }

  function submit(firstValue: string, secondValue: string) {
    setIsSubmitting(true);
    setSubmitError(undefined);

    if (isChangingDefaultPassword) {
      const currentPassword = firstValue;
      const newPassword = secondValue;

      login({
        password: currentPassword,
        onSuccess: () => {
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
                  navigate("/onboarding/mining-pool");
                },
                onError: handleError,
              });
            },
            onError: handleError,
          });
        },
        onError: handleError,
      });
      return;
    }

    const password = firstValue;
    setPassword({
      password,
      onSuccess: () => {
        login({
          password,
          onSuccess: () => {
            setIsSubmitting(false);
            navigate("/onboarding/mining-pool");
          },
          onError: handleError,
        });
      },
      onError: handleError,
    });
  }

  return (
    <OnboardingLayout>
      <Authentication
        isUpdateMode={isChangingDefaultPassword}
        submit={submit}
        submitError={submitError}
        isSubmitting={isSubmitting}
        setIsSubmitting={setIsSubmitting}
        headline={isChangingDefaultPassword ? "Update your admin login" : "Create an admin login for your miner"}
        description={
          isChangingDefaultPassword
            ? "Your miner is still using the factory default password. Change it now to continue setup."
            : "This password is required to modify performance settings or mining pool configurations for this miner."
        }
        initUsername="admin"
      />
    </OnboardingLayout>
  );
};

export default AuthenticationPage;
