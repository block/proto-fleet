import { fireEvent, render, screen } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { Code } from "@connectrpc/connect";
import LoginForm from "./LoginForm";

const { login } = vi.hoisted(() => ({ login: vi.fn() }));
vi.mock("@/protoFleet/api/useLogin", () => ({
  useLogin: () => login,
}));

it.each([
  [
    Code.ResourceExhausted,
    "Too many password attempts. Try again in one minute.",
    "Too many password attempts. Try again in one minute.",
  ],
  [Code.Internal, "internal database error", "Invalid credentials entered."],
])("shows retry guidance only for throttling (%s)", (code, message, expected) => {
  login.mockImplementation(({ onError }) => onError(message, code));
  render(<LoginForm onSuccess={vi.fn()} />);
  fireEvent.click(screen.getByTestId("login-button"));
  expect(screen.getByText(expected)).toBeVisible();
});
