import { ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import {
  initErrors,
  initValues,
} from "@/shared/components/Setup/authentication.constants";
import { Values } from "@/shared/components/Setup/authentication.types";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import { deepClone } from "@/shared/utils/utility";

type WeakPasswordWarningProps = {
  onReturn: () => void;
  onContinue: () => void;
};

const WeakPasswordWarning = ({
  onReturn,
  onContinue,
}: WeakPasswordWarningProps) => {
  return (
    <Modal
      title="Your password isn't secure"
      description="The password you entered is easy to guess. We recommend creating a password that's harder to guess."
      preventClose
      size="small"
      className="!max-w-[360px]"
    >
      <div className="mt-4 flex grow-0 flex-col gap-3">
        <Button
          onClick={onReturn}
          size="base"
          variant="primary"
          className="w-full"
        >
          Create a stronger password
        </Button>
        <Button
          onClick={onContinue}
          size="base"
          variant="secondary"
          className="w-full"
        >
          Continue anyway
        </Button>
      </div>
    </Modal>
  );
};

export const PasswordStrengthMeter = ({
  password,
  score,
  onSetScore,
}: {
  password: string;
  score: number;
  onSetScore: (score: number) => void;
}) => {
  const calculatePasswordScore = (password: string): number => {
    if (!password) return 0;

    let score = 0;
    const letters = new Map<string, number>();

    password.split("").forEach((char) => {
      const count = (letters.get(char) || 0) + 1;
      letters.set(char, count);
      score += 5 / Math.min(count, 5);
    });

    const variationCount = [
      /\d/.test(password), // digits
      /[a-z]/.test(password), // lowercase
      /[A-Z]/.test(password), // uppercase
      /\W/.test(password), // non-word characters
    ].filter(Boolean).length;

    score += (variationCount - 1) * 10;

    return Math.floor(score);
  };

  useEffect(() => {
    onSetScore(calculatePasswordScore(password));
  }, [password, onSetScore]);

  return (
    <div className="flex gap-1">
      <div
        className={clsx("h-1 w-[18px] rounded-full", {
          "bg-core-primary-10": score === 0,
          "bg-intent-critical-fill": score > 0 && score < 50,
          "bg-intent-warning-fill": score >= 50 && score < 90,
          "bg-intent-success-fill": score >= 90,
        })}
      />
      <div
        className={clsx("h-1 w-[18px] rounded-full", {
          "bg-core-primary-10": score < 50,
          "bg-intent-warning-fill": score >= 50 && score < 90,
          "bg-intent-success-fill": score >= 90,
        })}
      />
      <div
        className={clsx("h-1 w-[18px] rounded-full", {
          "bg-core-primary-10": score < 90,
          "bg-intent-success-fill": score >= 90,
        })}
      />
    </div>
  );
};

type AuthenticationProps = {
  headline: string | ReactNode;
  description?: string;
  initUsername?: string;
  inputPrefix?: string;
  submit:
    | ((password: string, username: string) => void)
    | ((currentPassword: string, newPassword: string) => void);
  isUpdateMode?: boolean;
  requirePasswordConfirmation?: boolean;
  buttonClassName?: string;
  isSubmitting: boolean;
  setIsSubmitting: (isSubmitting: boolean) => void;
  submitError?: string;
};

const Authentication = ({
  headline,
  description,
  inputPrefix,
  initUsername,
  submit,
  isUpdateMode = false,
  requirePasswordConfirmation = true,
  buttonClassName = "ml-auto",
  isSubmitting,
  setIsSubmitting,
  submitError,
}: AuthenticationProps) => {
  const defaultValues = deepClone(initValues);
  if (initUsername !== undefined) {
    defaultValues.username = initUsername;
  }
  const [values, setValues] = useState<Values>(defaultValues);

  const [passwordsMatch, setPasswordsMatch] = useState<boolean>(false);
  const [errors, setErrors] = useState<Values>(deepClone(initErrors));
  const [score, setScore] = useState(0);
  const [showWeakPasswordWarning, setShowWeakPasswordWarning] = useState(false);

  const validate = useCallback(() => {
    let newErrors: Values = deepClone(initErrors);

    if (values.username.length === 0) {
      newErrors.username = "A username is required";
    }
    if (
      isUpdateMode &&
      (!values.currentPassword || values.currentPassword.length === 0)
    ) {
      newErrors.currentPassword = "Current password is required";
    }
    if (values.password.length === 0) {
      newErrors.password = "A password is required";
    }
    if (
      requirePasswordConfirmation &&
      values.confirmPassword !== values.password
    ) {
      newErrors.confirmPassword = "Passwords do not match";
    }

    setErrors(newErrors);
    return Object.values(newErrors).some((err) => err.length > 0);
  }, [
    values.username,
    values.currentPassword,
    values.password,
    values.confirmPassword,
    isUpdateMode,
    requirePasswordConfirmation,
  ]);

  const handleContinue = useCallback(
    (forcedWeakPassword: boolean) => {
      const hasValidationErrors = validate();

      if (!hasValidationErrors) {
        if (!forcedWeakPassword && score < 50) {
          setShowWeakPasswordWarning(true);
          return;
        }
        setShowWeakPasswordWarning(false);
        setIsSubmitting(true);
        try {
          if (isUpdateMode) {
            if (!values.currentPassword) return;

            (submit as (currentPassword: string, newPassword: string) => void)(
              values.currentPassword,
              values.password,
            );
          } else {
            (submit as (password: string, username: string) => void)(
              values.password,
              values.username,
            );
          }
        } finally {
          setIsSubmitting(false);
        }
      }
    },
    [
      validate,
      score,
      setIsSubmitting,
      isUpdateMode,
      values.currentPassword,
      values.password,
      values.username,
      submit,
    ],
  );

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      setErrors(deepClone(initErrors));
    },
    [values],
  );

  useEffect(() => {
    if (
      values.password === values.confirmPassword &&
      values.password.length > 0
    ) {
      setPasswordsMatch(true);
    } else {
      setPasswordsMatch(false);
    }
  }, [values]);

  const hasErrors = useMemo(
    () => Object.values(errors).some((err) => err.length > 0),
    [errors],
  );

  const disableContinue = useMemo(() => {
    return hasErrors || isSubmitting;
  }, [hasErrors, isSubmitting]);

  const handleEnter = useCallback(() => {
    if (disableContinue) {
      return;
    }

    handleContinue(false);
  }, [disableContinue, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div className="flex flex-col gap-6">
      <Header
        title={headline}
        titleSize="text-heading-300"
        description={description}
      />
      <div
        className={clsx("transition-[max-height,margin] ease-in-out", {
          "max-h-0 overflow-hidden duration-300": !submitError,
          "max-h-96 duration-500": submitError,
        })}
      >
        <div className="rounded-lg bg-intent-critical-10 px-3 py-2 text-emphasis-300 leading-5 text-intent-critical-text">
          {submitError}
        </div>
      </div>
      <Input
        onChange={handleChange}
        id="username"
        label={inputPrefix ? `${inputPrefix} username` : "Username"}
        disabled={initUsername !== undefined && initUsername !== ""}
        initValue={values.username}
        error={errors.username}
      />
      {isUpdateMode && (
        <Input
          onChange={handleChange}
          id="currentPassword"
          label="Current password"
          type="password"
          initValue={values.currentPassword}
          error={errors.currentPassword}
        />
      )}
      <div className="space-y-2">
        <Input
          onChange={handleChange}
          id="password"
          label={inputPrefix ? `${inputPrefix} password` : "Password"}
          type="password"
          initValue={values.password}
          error={errors.password}
        />
        <div className="flex items-center justify-between gap-5">
          <div>
            <div className="text-200 text-text-primary-50">
              Password strength
            </div>
          </div>
          <PasswordStrengthMeter
            score={score}
            onSetScore={setScore}
            password={values.password}
          />
        </div>
      </div>
      {requirePasswordConfirmation && (
        <Input
          onChange={handleChange}
          id="confirmPassword"
          label="Confirm password"
          type="password"
          initValue={values.confirmPassword}
          error={errors.confirmPassword}
          statusIcon={
            passwordsMatch ? (
              <Success className="text-intent-success-fill" />
            ) : undefined
          }
        />
      )}
      {showWeakPasswordWarning && !isSubmitting && (
        <WeakPasswordWarning
          onReturn={() => setShowWeakPasswordWarning(false)}
          onContinue={() => handleContinue(true)}
        />
      )}
      <Button
        onClick={() => handleContinue(false)}
        className={buttonClassName}
        size={sizes.base}
        variant={variants.primary}
        loading={isSubmitting}
      >
        Continue
      </Button>
    </div>
  );
};

export default Authentication;
