import { useCallback, useState } from "react";
import clsx from "clsx";

import { useLogin } from "@/protoOS/api";

import { ids, initValues, type Values } from "@/protoOS/pages/Auth";
import { variants } from "@/shared/components/Button";
import ButtonGroup, {
  ButtonProps,
  groupVariants,
  sizes,
} from "@/shared/components/ButtonGroup";
import Input from "@/shared/components/Input";
import { useKeyDown } from "@/shared/hooks/useKeyDown";

// TODO: When implemement Auth for ProtoFleet we may want move these
// consts elsewhere because the components shouldnt be importing from pages
// leaving for now because Auth will probably be moved shared/features
import { deepClone } from "@/shared/utils/utility";

interface LoginFormProps {
  onClickForgotPassword: () => void;
  onDismiss?: () => void;
  onSuccess: () => void;
}

const LoginForm = ({
  onClickForgotPassword,
  onDismiss,
  onSuccess,
}: LoginFormProps) => {
  const [values, setValues] = useState<Values>(deepClone(initValues));
  const [errors, setErrors] = useState<Values>(deepClone(initValues));
  const [apiError, setApiError] = useState<string | null>(null);
  const { login } = useLogin();
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleChange = useCallback(
    (value: string, id: string) => {
      setValues({ ...values, [id]: value.trim() });
      // clear errors if the user starts typing
      setErrors(deepClone(initValues));
      setApiError(null);
    },
    [values]
  );

  const handleContinue = useCallback(() => {
    setIsSubmitting(true);
    login({
      password: values.password,
      onSuccess,
      onError: () => setApiError("Invalid credentials entered."),
      onFinally: () => setIsSubmitting(false),
    });
  }, [onSuccess, login, values.password]);

  const handleEnter = useCallback(() => {
    if (isSubmitting) {
      return;
    }

    handleContinue();
  }, [isSubmitting, handleContinue]);

  useKeyDown({ key: "Enter", onKeyDown: handleEnter });

  return (
    <div data-testid="login-form">
      <div className="text-heading-200 text-text-primary">Login required</div>
      <div className="text-300 text-text-primary-70 mb-4 mt-1">
        Contact your system administrator if you need access.
      </div>

      <div
        className={clsx("transition-[max-height,margin] ease-in-out", {
          "max-h-0 overflow-hidden duration-300": !apiError,
          "max-h-96 mb-4 duration-500": apiError,
        })}
        data-testid="error"
      >
        <div className="bg-intent-critical-10 text-intent-critical-text text-emphasis-300 px-3 py-2 rounded-lg">
          Invalid credentials entered.
        </div>
      </div>

      <div className="bg-surface-elevated-base rounded-lg relative z-10">
        <Input
          id={ids.username}
          label="Username"
          initValue="admin"
          disabled
          className="mb-4"
          testId="username"
        />
      </div>

      <Input
        id={ids.password}
        label="Password"
        onChange={handleChange}
        type="password"
        initValue={values.password}
        error={errors.password}
        className="mb-2"
        testId="password"
        autoFocus
      />

      <div
        className="flex text-200 text-text-primary-50 mb-4 hover:cursor-pointer"
        onClick={onClickForgotPassword}
        data-testid="forgot-password"
      >
        {"Forgot password ->"}
      </div>

      <ButtonGroup
        variant={groupVariants.fill}
        size={sizes.base}
        buttons={
          [
            {
              ...(onDismiss && {
                text: "Cancel",
                onClick: onDismiss,
                variant: variants.secondary,
              }),
            },
            {
              text: "Continue",
              onClick: handleContinue,
              variant: variants.primary,
              disabled: isSubmitting,
              testId: "login-button",
            },
          ].filter((button) => !!button.text) as ButtonProps[]
        }
      />
    </div>
  );
};

export default LoginForm;
