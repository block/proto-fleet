import { useState } from "react";
import { action } from "storybook/actions";
import { UpdatePasswordForm } from "./UpdatePasswordForm";

export default {
  title: "Proto Fleet/Auth/UpdatePasswordForm",
  component: UpdatePasswordForm,
};

// Default story
export const Default = () => {
  return (
    <UpdatePasswordForm
      onSubmit={(newPassword, confirmPassword) => {
        action("onSubmit")(newPassword, confirmPassword);
      }}
      isSubmitting={false}
      errorMsg=""
    />
  );
};

// With error message
export const WithError = () => {
  return (
    <UpdatePasswordForm
      onSubmit={(newPassword, confirmPassword) => {
        action("onSubmit")(newPassword, confirmPassword);
      }}
      isSubmitting={false}
      errorMsg="Passwords do not match. Please try again."
    />
  );
};

// With validation error
export const WithWeakPasswordError = () => {
  return (
    <UpdatePasswordForm
      onSubmit={(newPassword, confirmPassword) => {
        action("onSubmit")(newPassword, confirmPassword);
      }}
      isSubmitting={false}
      errorMsg="Password must be at least 8 characters and include uppercase, lowercase, number, and special character."
    />
  );
};

// Loading state
export const LoadingState = () => {
  return (
    <UpdatePasswordForm
      onSubmit={(newPassword, confirmPassword) => {
        action("onSubmit")(newPassword, confirmPassword);
      }}
      isSubmitting={true}
      errorMsg=""
    />
  );
};

// Interactive demo
export const Interactive = () => {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState("");

  const handleSubmit = (newPassword: string, confirmPassword: string) => {
    action("onSubmit")(newPassword, confirmPassword);
    setErrorMsg("");

    // Validate passwords match
    if (newPassword !== confirmPassword) {
      setErrorMsg("Passwords do not match. Please try again.");
      return;
    }

    // Validate password strength (basic check)
    if (newPassword.length < 8) {
      setErrorMsg("Password must be at least 8 characters long.");
      return;
    }

    // Simulate API call
    setIsSubmitting(true);
    setTimeout(() => {
      setIsSubmitting(false);
      action("Success!")();
    }, 2000);
  };

  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-warning-10 p-4 text-300 text-text-primary">
        Try entering mismatched passwords or a weak password to see validation errors. Enter matching strong passwords
        to simulate success (2 second delay).
      </div>
      <UpdatePasswordForm
        onSubmit={handleSubmit}
        isSubmitting={isSubmitting}
        errorMsg={errorMsg}
        onErrorDismiss={() => setErrorMsg("")}
      />
    </div>
  );
};

// With strong password
export const WithStrongPassword = () => {
  const [errorMsg, setErrorMsg] = useState("");

  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-success-10 p-4 text-300 text-text-primary">
        Try entering: "StrongP@ssw0rd" to see a strong password score
      </div>
      <UpdatePasswordForm
        onSubmit={(newPassword, confirmPassword) => {
          action("onSubmit")(newPassword, confirmPassword);
        }}
        isSubmitting={false}
        errorMsg={errorMsg}
        onErrorDismiss={() => setErrorMsg("")}
      />
    </div>
  );
};
