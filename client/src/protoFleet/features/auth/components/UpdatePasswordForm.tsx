import { useState } from "react";
import Footer from "@/protoFleet/components/Footer";
import { Logo } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import { PasswordStrengthMeter } from "@/shared/components/Setup";

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

  const handlePasswordChange = (value: string) => {
    setNewPassword(value);
    onErrorDismiss?.();
  };

  const handleConfirmPasswordChange = (value: string) => {
    setConfirmPassword(value);
    onErrorDismiss?.();
  };

  const handleSubmit = () => {
    onSubmit(newPassword, confirmPassword);
  };

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

              {errorMsg ? (
                <div className="rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 text-intent-critical-text">
                  {errorMsg}
                </div>
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

              <Button onClick={handleSubmit} variant="primary" disabled={isSubmitting}>
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
