import { useCallback, useState } from "react";
import Footer from "@/protoFleet/components/Footer";
import { Alert, Logo } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import { PasswordStrengthMeter, WeakPasswordWarning } from "@/shared/components/Setup";
import { isPasswordTooShort, isWeakPassword, passwordErrors } from "@/shared/components/Setup/authentication.constants";

interface UpdatePasswordFormProps {
  onSubmit: (newPassword: string, confirmPassword: string) => void;
  isSubmitting?: boolean;
  errorMsg?: string;
  onErrorDismiss?: () => void;
}

export const UpdatePasswordForm = ({
  onSubmit,
  isSubmitting = false,
  errorMsg = "",
  onErrorDismiss,
}: UpdatePasswordFormProps) => {
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [score, setScore] = useState(0);
  const [validationError, setValidationError] = useState("");
  const [showWeakPasswordWarning, setShowWeakPasswordWarning] = useState(false);

  const handlePasswordChange = (value: string) => {
    setNewPassword(value);
    setValidationError("");
    onErrorDismiss?.();
  };

  const handleConfirmPasswordChange = (value: string) => {
    setConfirmPassword(value);
    setValidationError("");
    onErrorDismiss?.();
  };

  const handleSubmit = useCallback(
    (forcedWeakPassword: boolean) => {
      // Validate password length
      if (isPasswordTooShort(newPassword)) {
        setValidationError(passwordErrors.tooShort);
        return;
      }

      // Validate passwords match
      if (newPassword !== confirmPassword) {
        setValidationError(passwordErrors.mismatch);
        return;
      }

      // Check for weak password
      if (!forcedWeakPassword && isWeakPassword(score)) {
        setShowWeakPasswordWarning(true);
        return;
      }

      setShowWeakPasswordWarning(false);
      onSubmit(newPassword, confirmPassword);
    },
    [newPassword, confirmPassword, score, onSubmit],
  );

  return (
    <div className="flex h-screen w-full flex-col bg-surface-base">
      <div className="flex flex-grow items-center-safe justify-center-safe">
        <div className="w-full max-w-100 p-6 phone:h-full">
          <div className="flex flex-col gap-10">
            <Logo width="w-[86px]" />
            <div className="flex flex-col gap-6">
              <Header
                title="Update Your Password"
                titleSize="text-heading-300"
                description="You logged in with a temporary password. Enter your new password to continue."
              />

              {errorMsg || validationError ? (
                <Callout intent="danger" prefixIcon={<Alert />} title={errorMsg || validationError} />
              ) : null}

              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-2">
                  <Input id="newPassword" label="New password" type="password" onChange={handlePasswordChange} />
                  <div className="flex items-center justify-between gap-5">
                    <div>
                      <div className="text-200 text-text-primary-50">Password strength</div>
                    </div>
                    <PasswordStrengthMeter score={score} onSetScore={setScore} password={newPassword} />
                  </div>
                </div>

                <Input
                  id="confirmPassword"
                  label="Confirm password"
                  type="password"
                  onChange={handleConfirmPasswordChange}
                />
              </div>

              {showWeakPasswordWarning && !isSubmitting ? (
                <WeakPasswordWarning
                  onReturn={() => setShowWeakPasswordWarning(false)}
                  onContinue={() => handleSubmit(true)}
                />
              ) : null}

              <Button onClick={() => handleSubmit(false)} variant="primary" disabled={isSubmitting}>
                {isSubmitting ? "Updating..." : "Continue"}
              </Button>
            </div>
          </div>
        </div>
      </div>
      <Footer />
    </div>
  );
};
