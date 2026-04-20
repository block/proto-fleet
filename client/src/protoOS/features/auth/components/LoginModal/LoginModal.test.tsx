import { fireEvent, render, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, test, vi } from "vitest";
import LoginModal from "./LoginModal";
import { MinerHostingProvider } from "@/protoOS/contexts/MinerHostingContext";

describe("Login Modal", () => {
  const loginForm = "login-form";
  const loginButton = "login-button";
  const forgotPassword = "forgot-password";
  const forgotPasswordInstructions = "forgot-password-instructions";
  const forgotPasswordBack = "forgot-password-back";
  const username = "username";
  const password = "password";
  const error = "error";
  const hiddenErrorClass = "max-h-0";
  const visibleErrorClass = "max-h-96";
  const eyeIcon = "eye-icon";
  const lessThan8Characters = "1234567";
  const validPassword = "12345678";

  let component: ReturnType<typeof render>;

  beforeEach(() => {
    component = render(
      <MinerHostingProvider>
        <LoginModal onSuccess={vi.fn} onDismiss={vi.fn} />
      </MinerHostingProvider>,
    );
  });

  test("renders login modal with login form", () => {
    const { getByTestId } = component;
    expect(getByTestId(loginForm)).toBeInTheDocument();
  });

  test("renders login modal with forgot password instructions", () => {
    const { getByTestId } = component;
    fireEvent.click(getByTestId(forgotPassword));
    expect(getByTestId(forgotPasswordInstructions)).toBeInTheDocument();
  });

  test("closes forgot password instructions on click of back button", async () => {
    const { getByTestId, queryByTestId } = component;
    fireEvent.click(getByTestId(forgotPassword));
    fireEvent.click(getByTestId(forgotPasswordBack));
    await waitFor(() => expect(queryByTestId(forgotPasswordInstructions)).not.toBeInTheDocument());
  });

  test("username field is non-editable", () => {
    const { getByTestId } = component;
    expect(getByTestId(username)).toHaveAttribute("disabled");
  });

  test("password field is editable", () => {
    const { getByTestId } = component;
    fireEvent.change(getByTestId(password), {
      target: { value: validPassword },
    });
    expect(getByTestId(password)).toHaveValue(validPassword);
  });

  test("password field validation fires on empty value", async () => {
    const { getByTestId } = component;
    expect(getByTestId(error)).toHaveClass(hiddenErrorClass);
    fireEvent.click(getByTestId(loginButton));
    await waitFor(() => {
      expect(getByTestId(error)).toHaveClass(visibleErrorClass);
    });
  });

  test("password field validation fires on less than 8 characters", async () => {
    const { getByTestId } = component;
    fireEvent.change(getByTestId(password), {
      target: { value: lessThan8Characters },
    });
    expect(getByTestId(error)).toHaveClass(hiddenErrorClass);
    fireEvent.click(getByTestId(loginButton));
    await waitFor(() => {
      expect(getByTestId(error)).toHaveClass(visibleErrorClass);
    });
  });

  test("password field validation hides on change of password", async () => {
    const { getByTestId } = component;
    fireEvent.click(getByTestId(loginButton));
    await waitFor(() => {
      expect(getByTestId(error)).toHaveClass(visibleErrorClass);
    });
    fireEvent.change(getByTestId(password), {
      target: { value: lessThan8Characters },
    });
    await waitFor(() => {
      expect(getByTestId(error)).toHaveClass(hiddenErrorClass);
    });
    fireEvent.click(getByTestId(loginButton));
    await waitFor(() => {
      expect(getByTestId(error)).toHaveClass(visibleErrorClass);
    });
  });

  test("clicking the eye icon shows/hides the password", async () => {
    const { getByTestId } = component;
    expect(getByTestId(password)).toHaveAttribute("type", "password");
    fireEvent.click(getByTestId(eyeIcon));
    expect(getByTestId(password)).toHaveAttribute("type", "text");
  });
});
