import { ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import { Alert, Success } from "@/shared/assets/icons";
import Button, { sizes, variants } from "@/shared/components/Button";
import Callout from "@/shared/components/Callout";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import {
  initErrors,
  initValues,
  isPasswordTooShort,
  isWeakPassword,
  passwordErrors,
} from "@/shared/components/Setup/authentication.constants";
import { Values } from "@/shared/components/Setup/authentication.types";
import { useKeyDown } from "@/shared/hooks/useKeyDown";
import { deepClone } from "@/shared/utils/utility";

export type WeakPasswordWarningProps = {
  onReturn: () => void;
  onContinue: () => void;
};

export const WeakPasswordWarning = ({ onReturn, onContinue }: WeakPasswordWarningProps) => {
  return (
    <Modal
      title="Your password isn't secure"
      description="The password you entered is easy to guess. We recommend creating a password that's harder to guess."
      divider={false}
      icon={null}
    >
      <div className="mt-4 flex grow-0 flex-col gap-3">
        <Button onClick={onReturn} size="base" variant="primary" className="w-full">
          Create a stronger password
        </Button>
        <Button onClick={onContinue} size="base" variant="secondary" className="w-full">
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
          "bg-intent-critical-fill": score > 0 && isWeakPassword(score),
          "bg-intent-warning-fill": !isWeakPassword(score) && score < 90,
          "bg-intent-success-fill": score >= 90,
        })}
      />
      <div
        className={clsx("h-1 w-[18px] rounded-full", {
          "bg-core-primary-10": isWeakPassword(score),
          "bg-intent-warning-fill": !isWeakPassword(score) && score < 90,
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
  submit: ((password: string, username: string) => void) | ((currentPassword: string, newPassword: string) => void);
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

  // Derive passwordsMatch from values instead of storing in state
  const passwordsMatch = useMemo(
    () => values.password === values.confirmPassword && values.password.length > 0,
    [values.password, values.confirmPassword],
  );
  const [errors, setErrors] = useState<Values>(deepClone(initErrors));
  const [score, setScore] = useState(0);
  const [showWeakPasswordWarning, setShowWeakPasswordWarning] = useState(false);

  const validate = useCallback(() => {
    let newErrors: Values = deepClone(initErrors);

    if (values.username.length === 0) {
      newErrors.username = passwordErrors.usernameRequired;
    }
    if (isUpdateMode && (!values.currentPassword || values.currentPassword.length === 0)) {
      newErrors.currentPassword = passwordErrors.currentPasswordRequired;
    }
    if (values.password.length === 0) {
      newErrors.password = passwordErrors.required;
    } else if (isPasswordTooShort(values.password)) {
      newErrors.password = passwordErrors.tooShort;
    }
    if (requirePasswordConfirmation && values.confirmPassword !== values.password) {
      newErrors.confirmPassword = passwordErrors.mismatch;
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
        if (!forcedWeakPassword && isWeakPassword(score)) {
          setShowWeakPasswordWarning(true);
          return;
        }
        setShowWeakPasswordWarning(false);
        setIsSubmitting(true);
        if (isUpdateMode) {
          if (!values.currentPassword) return;

          (submit as (currentPassword: string, newPassword: string) => void)(values.currentPassword, values.password);
        } else {
          (submit as (password: string, username: string) => void)(values.password, values.username);
        }
      }
    },
    [validate, score, setIsSubmitting, isUpdateMode, values.currentPassword, values.password, values.username, submit],
  );

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      setErrors(deepClone(initErrors));
    },
    [values],
  );

  const hasErrors = useMemo(() => Object.values(errors).some((err) => err.length > 0), [errors]);

  const disableContinue = useMemo(() => {
    return isPasswordTooShort(values.password) || hasErrors || isSubmitting;
  }, [values.password, hasErrors, isSubmitting]);

  const handleEnter = useCallback(() => {
    if (disableContinue) {
      return;
    }

    handleContinue(false);
  }, [disableContinue, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div className="flex flex-col gap-6">
      <Header title={headline} titleSize="text-heading-300" description={description} />
      <div
        className={clsx("transition-[max-height,margin] ease-in-out", {
          "max-h-0 overflow-hidden duration-300": !submitError,
          "max-h-96 duration-500": submitError,
        })}
      >
        <Callout intent="danger" prefixIcon={<Alert />} title={submitError} />
      </div>
      <div className="flex flex-col gap-6 bg-surface-base">
        <Input
          onChange={handleChange}
          id="username"
          label={inputPrefix ? `${inputPrefix} username` : "Username"}
          disabled={initUsername !== undefined ? initUsername !== "" : false}
          initValue={values.username}
          error={errors.username}
          autoFocus={!initUsername}
        />
        {isUpdateMode ? (
          <Input
            onChange={handleChange}
            id="currentPassword"
            label="Current password"
            type="password"
            initValue={values.currentPassword}
            error={errors.currentPassword}
            autoFocus={!!initUsername}
          />
        ) : null}
        <div className="space-y-2">
          <Input
            onChange={handleChange}
            id="password"
            label={inputPrefix ? `${inputPrefix} password` : "Password"}
            type="password"
            initValue={values.password}
            error={errors.password}
            autoFocus={initUsername ? !isUpdateMode : false}
          />
          <div className="flex items-center justify-between gap-5">
            <div>
              <div className="text-200 text-text-primary-50">Password strength</div>
            </div>
            <PasswordStrengthMeter score={score} onSetScore={setScore} password={values.password} />
          </div>
        </div>
      </div>
      {requirePasswordConfirmation ? (
        <Input
          onChange={handleChange}
          id="confirmPassword"
          label="Confirm password"
          type="password"
          initValue={values.confirmPassword}
          error={errors.confirmPassword}
          statusIcon={passwordsMatch ? <Success className="text-intent-success-fill" /> : undefined}
        />
      ) : null}
      {showWeakPasswordWarning && !isSubmitting ? (
        <WeakPasswordWarning
          onReturn={() => setShowWeakPasswordWarning(false)}
          onContinue={() => handleContinue(true)}
        />
      ) : null}
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
