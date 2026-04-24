import { useCallback, useState } from "react";
import clsx from "clsx";
import { create } from "@bufbuild/protobuf";
import { AuthenticateRequestSchema } from "@/protoFleet/api/generated/auth/v1/auth_pb";
import { useAuth } from "@/protoFleet/api/useAuth";
import { useLogin } from "@/protoFleet/api/useLogin";
import { useUsername } from "@/protoFleet/store";
import { Alert } from "@/shared/assets/icons";
import Button from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import Row from "@/shared/components/Row";
import { PasswordStrengthMeter, WeakPasswordWarning } from "@/shared/components/Setup";
import { isPasswordTooShort, isWeakPassword, passwordErrors } from "@/shared/components/Setup/authentication.constants";
import { pushToast, STATUSES as TOAST_STATUSES } from "@/shared/features/toaster";

const AuthenticateForm = ({ onChange, apiError }: { onChange: (value: string) => void; apiError: string | null }) => {
  return (
    <div className="flex flex-col gap-6">
      <Header
        title="Account password required"
        titleSize="text-heading-300"
        description="For account protection, your current Fleet account password is required to save changes to your settings."
      />
      <div>
        <div
          className={clsx("transition-[max-height,margin] ease-in-out", {
            "max-h-0 overflow-hidden duration-300": !apiError,
            "max-h-96 duration-500": apiError,
          })}
          data-testid="error"
        >
          <Callout className="mb-4" intent="danger" prefixIcon={<Alert />} title={apiError} />
        </div>

        <Input id="currentPassword" label="Password" type="password" onChange={onChange} autoFocus />
      </div>
    </div>
  );
};

const FormattedDate = ({ date, className, label }: { date: Date | null; className?: string; label?: string }) => {
  return (
    <span className={className}>
      {label ? <>{label} </> : null}
      {date?.toLocaleString(undefined, {
        month: "short",
        day: "2-digit",
        year: "numeric",
      })}
    </span>
  );
};

const AuthenticationSettings = () => {
  const username = useUsername();

  const { updatePassword, updateUsername, passwordLastUpdatedAt } = useAuth();
  const login = useLogin();

  const [showModal, setShowModal] = useState(false);
  const [updatingState, setUpdatingState] = useState<"password" | "username">();
  const [step, setStep] = useState<"authenticate" | "updatePassword" | "updateUsername">("authenticate");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const [password, setPassword] = useState("");
  const [score, setScore] = useState(0);
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [passwordErrorMsg, setPasswordErrorMsg] = useState("");
  const [newUsername, setNewUsername] = useState("");
  const [usernameErrorMsg, setUsernameErrorMsg] = useState("");

  // API error states
  const [authApiError, setAuthApiError] = useState<string | null>(null);
  const [passwordUpdateApiError, setPasswordUpdateApiError] = useState<string | null>(null);
  const [usernameUpdateApiError, setUsernameUpdateApiError] = useState<string | null>(null);
  const [showWeakPasswordWarning, setShowWeakPasswordWarning] = useState(false);

  // Reset form state when modal closes
  const [prevShowModal, setPrevShowModal] = useState(showModal);
  if (prevShowModal !== showModal) {
    setPrevShowModal(showModal);
    if (!showModal) {
      setStep("authenticate");
      setIsSubmitting(false);
      setPassword("");
      setScore(0);
      setNewPassword("");
      setConfirmPassword("");
      setPasswordErrorMsg("");
      setNewUsername("");
      setUsernameErrorMsg("");
      setAuthApiError(null);
      setPasswordUpdateApiError(null);
      setUsernameUpdateApiError(null);
      setShowWeakPasswordWarning(false);
    }
  }

  // Clear errors when user starts typing
  const handlePasswordChange = (value: string) => {
    setPassword(value);
    setAuthApiError(null);
  };

  const handleNewPasswordChange = (value: string) => {
    setNewPassword(value);
    setPasswordErrorMsg("");
    setPasswordUpdateApiError(null);
  };

  const handleConfirmPasswordChange = (value: string) => {
    setConfirmPassword(value);
    setPasswordErrorMsg("");
    setPasswordUpdateApiError(null);
  };

  const handleNewUsernameChange = (value: string) => {
    setNewUsername(value);
    setUsernameErrorMsg("");
    setUsernameUpdateApiError(null);
  };

  function authenticate() {
    setIsSubmitting(true);
    setAuthApiError(null); // Clear any previous error
    login({
      loginRequest: create(AuthenticateRequestSchema, { username, password }),
      skipLogoutOnError: true,
      onSuccess: () => {
        if (updatingState === "password") {
          setStep("updatePassword");
        } else if (updatingState === "username") {
          setStep("updateUsername");
        }
      },
      onError: () => {
        setAuthApiError("Authentication failed. Please check your password and try again.");
      },
      onFinally: () => {
        setIsSubmitting(false);
      },
    });
  }

  const submitPasswordUpdate = useCallback(
    (forcedWeakPassword: boolean) => {
      // Validate password length
      if (isPasswordTooShort(newPassword)) {
        setPasswordErrorMsg(passwordErrors.tooShort);
        return;
      }

      // Validate passwords match
      if (newPassword !== confirmPassword) {
        setPasswordErrorMsg(passwordErrors.mismatch);
        return;
      }

      // Check for weak password
      if (!forcedWeakPassword && isWeakPassword(score)) {
        setShowWeakPasswordWarning(true);
        return;
      }

      setShowWeakPasswordWarning(false);
      setIsSubmitting(true);
      setPasswordErrorMsg("");
      setPasswordUpdateApiError(null);

      updatePassword({
        currentPassword: password,
        newPassword: newPassword,
        onSuccess: () => {
          login({
            loginRequest: create(AuthenticateRequestSchema, {
              username,
              password: newPassword,
            }),
            onSuccess: () => {
              pushToast({
                message: "Password updated",
                status: TOAST_STATUSES.success,
              });
              setShowModal(false);
            },
            onError: () => {
              setPasswordUpdateApiError("Password updated but re-login failed. Please log in again.");
            },
            onFinally: () => {
              setIsSubmitting(false);
            },
          });
        },
        onError: (error: string) => {
          setPasswordUpdateApiError(error || "Failed to update password. Please try again.");
          setIsSubmitting(false);
        },
      });
    },
    [newPassword, confirmPassword, score, password, username, updatePassword, login],
  );

  function submitUsernameUpdate() {
    // Validate username is not empty
    if (!newUsername) {
      setUsernameErrorMsg("New username is required");
      return;
    }

    setIsSubmitting(true);
    setUsernameErrorMsg("");
    setUsernameUpdateApiError(null);

    updateUsername({
      username: newUsername,
      onSuccess: () => {
        pushToast({
          message: "Username updated",
          status: TOAST_STATUSES.success,
        });
        setShowModal(false);
        setIsSubmitting(false);
      },
      onError: (error: string) => {
        setUsernameUpdateApiError(`Failed to update username: ${error}`);
        setIsSubmitting(false);
      },
    });
  }

  return (
    <>
      <div className="flex flex-col gap-6">
        <Header
          title="Security"
          titleSize="text-heading-300"
          description="Protect your mining fleet by managing system access, miner credentials, and team permissions."
        />
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-4 rounded-xl border border-border-5 p-6">
            <Header title="Account" titleSize="text-heading-200" />
            <div>
              <Row className="flex items-center justify-between gap-5" divider testId="username-row">
                <div className="text-emphasis-300">Username</div>
                <div className="flex items-center gap-3">
                  <span className="text-300" data-testid="username-value">
                    {username}
                  </span>
                  <Button
                    onClick={() => {
                      setShowModal(true);
                      setUpdatingState("username");
                      setStep("authenticate");
                      // Clear any previous errors
                      setAuthApiError(null);
                      setUsernameUpdateApiError(null);
                      setUsernameErrorMsg("");
                    }}
                    className="!p-0"
                    variant="textOnly"
                  >
                    Update
                  </Button>
                </div>
              </Row>
              <Row divider={false} className="flex items-center justify-between gap-5" testId="password-row">
                <div className="text-emphasis-300">Password</div>
                <div className="flex items-center gap-3">
                  {passwordLastUpdatedAt ? (
                    <FormattedDate className="text-300" label="Last updated" date={passwordLastUpdatedAt} />
                  ) : null}
                  <Button
                    onClick={() => {
                      setShowModal(true);
                      setUpdatingState("password");
                      setStep("authenticate");
                      // Clear any previous errors
                      setAuthApiError(null);
                      setPasswordUpdateApiError(null);
                      setPasswordErrorMsg("");
                    }}
                    className="!p-0"
                    variant="textOnly"
                  >
                    Update
                  </Button>
                </div>
              </Row>
            </div>
          </div>

          <Modal
            open={showModal}
            buttons={[
              {
                text: "Confirm",
                variant: "primary",
                dismissModalOnClick: false,
                loading: isSubmitting,
                disabled: false,
                onClick: () => {
                  if (step === "authenticate") {
                    authenticate();
                    return;
                  }

                  if (step === "updatePassword") {
                    submitPasswordUpdate(false);
                    return;
                  }

                  if (step === "updateUsername") {
                    submitUsernameUpdate();
                  }
                },
              },
            ]}
            divider={false}
            onDismiss={() => setShowModal(false)}
          >
            {step === "authenticate" && <AuthenticateForm onChange={handlePasswordChange} apiError={authApiError} />}
            {step === "updatePassword" && (
              <div className="flex flex-col gap-6">
                <Header
                  title="Update password"
                  titleSize="text-heading-300"
                  description="Your password will be used to log into Fleet."
                />

                <div>
                  <div
                    className={clsx("transition-[max-height,margin] ease-in-out", {
                      "max-h-0 overflow-hidden duration-300": !passwordUpdateApiError,
                      "max-h-96 duration-500": passwordUpdateApiError,
                    })}
                    data-testid="password-error"
                  >
                    <Callout className="mb-4" intent="danger" prefixIcon={<Alert />} title={passwordUpdateApiError} />
                  </div>

                  <div className="flex flex-col gap-4">
                    <div className="flex flex-col gap-2">
                      <Input
                        id="newPassword"
                        label="New password"
                        type="password"
                        onChange={handleNewPasswordChange}
                        error={passwordErrorMsg}
                        autoFocus
                      />
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
                </div>
                {showWeakPasswordWarning && !isSubmitting && (
                  <WeakPasswordWarning
                    onReturn={() => setShowWeakPasswordWarning(false)}
                    onContinue={() => submitPasswordUpdate(true)}
                  />
                )}
              </div>
            )}
            {step === "updateUsername" && (
              <div className="flex flex-col gap-6">
                <Header
                  title="Update username"
                  titleSize="text-heading-300"
                  description="Your username will be used to log into Fleet."
                />

                <div>
                  <div
                    className={clsx("transition-[max-height,margin] ease-in-out", {
                      "max-h-0 overflow-hidden duration-300": !usernameUpdateApiError,
                      "max-h-96 duration-500": usernameUpdateApiError,
                    })}
                    data-testid="username-error"
                  >
                    <Callout className="mb-4" intent="danger" prefixIcon={<Alert />} title={usernameUpdateApiError} />
                  </div>
                  <div className="flex flex-col gap-4">
                    <Input id="username" label="Username" type="text" disabled initValue={username} />
                    <Input
                      id="newUsername"
                      label="New username"
                      type="text"
                      onChange={handleNewUsernameChange}
                      error={usernameErrorMsg}
                      autoFocus
                    />
                  </div>
                </div>
              </div>
            )}
          </Modal>
        </div>
      </div>
    </>
  );
};

export default AuthenticationSettings;
