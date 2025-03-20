import { useCallback, useEffect, useMemo, useState } from "react";
import clsx from "clsx";
import Button from "@/shared/components/Button";
import Header from "@/shared/components/Header";
import Input from "@/shared/components/Input";
import Modal from "@/shared/components/Modal";
import { initValues } from "@/shared/components/Setup/authentication.constants";
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
      description="The password you entered is easy to guess. We recommend creating a password that’s harder to guess."
      preventClose
      className="max-w-sm"
    >
      <div className="mt-4 flex flex-col gap-3">
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

const PasswordStrengthMeter = ({
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
  submit: () => void;
};

const Authentication = ({ submit }: AuthenticationProps) => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [score, setScore] = useState(0);
  const [showWeakPasswordWarning, setShowWeakPasswordWarning] = useState(false);

  const validate = useCallback(() => {
    let newErrors: Values = deepClone(initValues);

    if (values.username.length === 0) {
      newErrors.username = "A username is required";
    }
    if (values.password.length === 0) {
      newErrors.password = "A password is required";
    }
    if (values.confirmPassword !== values.password) {
      newErrors.confirmPassword = "Passwords do not match";
    }

    setErrors(newErrors);
    return Object.values(newErrors).some((err) => err.length > 0);
  }, [values.username, values.password, values.confirmPassword]);

  const handleContinue = useCallback(
    (forcedWeakPassword: boolean) => {
      const hasValidationErrors = validate();

      if (!hasValidationErrors) {
        if (!forcedWeakPassword && score < 50) {
          setShowWeakPasswordWarning(true);
          return;
        }

        setIsSubmitting(true);
        try {
          submit();
        } catch {
          setIsSubmitting(false);
        }
      }
    },
    [validate, submit, score],
  );

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      // clear error if the user starts typing
      setErrors(deepClone(initValues));
    },
    [values],
  );

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
    <div className="container mx-auto max-w-xl">
      <div className="flex flex-col gap-6">
        <Header
          title="Create an admin login for your miners"
          titleSize="text-heading-300"
          description="This password is required to modify performance settings or mining pool configurations for this miner."
        />
        <Input
          onChange={handleChange}
          id="username"
          label="Username"
          initValue={values.username}
          error={errors.username}
        />
        <div className="space-y-2">
          <Input
            onChange={handleChange}
            id="password"
            label="Password"
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
        <Input
          onChange={handleChange}
          id="confirmPassword"
          label="Confirm password"
          type="password"
          initValue={values.confirmPassword}
          error={errors.confirmPassword}
        />
        {showWeakPasswordWarning && !isSubmitting && (
          <WeakPasswordWarning
            onReturn={() => setShowWeakPasswordWarning(false)}
            onContinue={() => handleContinue(true)}
          />
        )}
        <Button
          onClick={() => handleContinue(false)}
          className="ml-auto"
          size="base"
          variant="primary"
          loading={isSubmitting}
        >
          Continue
        </Button>
      </div>
    </div>
  );
};

export default Authentication;
