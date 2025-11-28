import { action } from "storybook/actions";
import { UpdatePasswordSuccess } from "./UpdatePasswordSuccess";

export default {
  title: "Proto Fleet/Auth/UpdatePasswordSuccess",
  component: UpdatePasswordSuccess,
};

// Default story
export const Default = () => {
  return (
    <UpdatePasswordSuccess
      onLogin={() => {
        action("onLogin")();
      }}
    />
  );
};

// Interactive demo
export const Interactive = () => {
  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-success-10 p-4 text-300 text-text-primary">
        Click the "Login" button to proceed to the login screen
      </div>
      <UpdatePasswordSuccess
        onLogin={() => {
          action("onLogin")();
          alert("Redirecting to login...");
        }}
      />
    </div>
  );
};
