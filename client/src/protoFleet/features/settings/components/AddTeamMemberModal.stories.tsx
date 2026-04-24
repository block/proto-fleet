import { useEffect, useState } from "react";
import { action } from "storybook/actions";
import AddTeamMemberModal from "./AddTeamMemberModal";
import { authClient } from "@/protoFleet/api/clients";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export default {
  title: "Proto Fleet/Settings/AddTeamMemberModal",
  component: AddTeamMemberModal,
};

// Helper to mock authClient.createUser for stories
const mockCreateUser = (delay: number, shouldSucceed: boolean, errorMessage?: string) => {
  const originalCreateUser = authClient.createUser;

  (authClient as any).createUser = async (request: any) => {
    action("createUser API called")(request);
    await new Promise((resolve) => setTimeout(resolve, delay));

    if (!shouldSucceed) {
      throw new Error(errorMessage || "Failed to create user");
    }

    return {
      userId: "user-123",
      username: request.username,
      temporaryPassword: "TempPass123!@#",
    };
  };

  return () => {
    (authClient as any).createUser = originalCreateUser;
  };
};

// Story wrapper to handle modal visibility
const StoryWrapper = ({
  mockDelay = 1000,
  shouldSucceed = true,
  errorMessage,
  infoMessage,
}: {
  mockDelay?: number;
  shouldSucceed?: boolean;
  errorMessage?: string;
  infoMessage?: string;
}) => {
  const [show, setShow] = useState(true);

  useEffect(() => {
    const cleanup = mockCreateUser(mockDelay, shouldSucceed, errorMessage);
    return cleanup;
  }, [mockDelay, shouldSucceed, errorMessage]);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div>
      {infoMessage ? (
        <div className="mb-4 rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">{infoMessage}</div>
      ) : null}
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <AddTeamMemberModal
        onSuccess={() => {
          action("onSuccess")();
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};

// Default interactive story - full flow
export const Default = () => (
  <StoryWrapper
    infoMessage="Enter a username and click Save to create a new team member. The flow will simulate a 1-second API call."
    mockDelay={1000}
    shouldSucceed={true}
  />
);

// Fast success - for quick testing
export const FastSuccess = () => (
  <StoryWrapper
    infoMessage="Quick success flow with 200ms delay for faster testing."
    mockDelay={200}
    shouldSucceed={true}
  />
);

// Slow API response
export const SlowResponse = () => (
  <StoryWrapper
    infoMessage="Simulates a slow API response (3 seconds) to test loading states."
    mockDelay={3000}
    shouldSucceed={true}
  />
);

// Error: Duplicate username
export const ErrorDuplicateUsername = () => (
  <StoryWrapper
    infoMessage="Try to create a user to see a duplicate username error."
    mockDelay={800}
    shouldSucceed={false}
    errorMessage="Username already exists"
  />
);

// Error: Invalid username
export const ErrorInvalidUsername = () => (
  <StoryWrapper
    infoMessage="Try to create a user to see an invalid username error."
    mockDelay={800}
    shouldSucceed={false}
    errorMessage="Username must be at least 3 characters"
  />
);

// Error: Generic error
export const ErrorGeneric = () => (
  <StoryWrapper
    infoMessage="Try to create a user to see a generic API error."
    mockDelay={800}
    shouldSucceed={false}
    errorMessage="Internal server error"
  />
);

// Testing empty username validation
export const EmptyUsernameValidation = () => {
  const [show, setShow] = useState(true);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-warning-10 p-4 text-300 text-text-primary">
        Click Save without entering a username to see validation error.
      </div>
      <AddTeamMemberModal
        onSuccess={() => {
          action("onSuccess")();
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};

// Long username test
export const LongUsername = () => {
  const [show, setShow] = useState(true);

  useEffect(() => {
    const cleanup = mockCreateUser(1000, true);
    return cleanup;
  }, []);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">
        Test with a very long username: "john_doe_with_a_very_long_username_for_testing_layout"
      </div>
      <AddTeamMemberModal
        onSuccess={() => {
          action("onSuccess")();
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};

// Visual test: Password display step
export const PasswordDisplayStep = () => {
  const [show, setShow] = useState(true);

  useEffect(() => {
    // Very fast response to quickly show password step
    const cleanup = mockCreateUser(100, true);
    return cleanup;
  }, []);

  if (!show) {
    return (
      <div className="flex h-screen items-center justify-center">
        <button onClick={() => setShow(true)} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
          Show Modal
        </button>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-4 rounded-lg bg-intent-success-10 p-4 text-300 text-text-primary">
        Enter any username and click Save to quickly see the password display step (100ms delay).
      </div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <AddTeamMemberModal
        onSuccess={() => {
          action("onSuccess")();
        }}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};
