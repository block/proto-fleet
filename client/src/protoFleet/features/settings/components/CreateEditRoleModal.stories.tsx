import { type ReactNode, useEffect, useState } from "react";
import { action } from "storybook/actions";
import CreateEditRoleModal from "./CreateEditRoleModal";
import { type RoleItem, useRoleManagement } from "@/protoFleet/api/useRoleManagement";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export default {
  title: "Proto Fleet/Settings/CreateEditRoleModal",
  component: CreateEditRoleModal,
};

const Banner = ({ children }: { children: ReactNode }) => (
  <div className="mb-4 rounded-lg bg-intent-info-10 p-4 text-300 text-text-primary">{children}</div>
);

const ToasterMount = () => (
  <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
    <ToasterComponent />
  </div>
);

const ShowAgain = ({ onShow }: { onShow: () => void }) => (
  <div className="flex h-screen items-center justify-center">
    <button onClick={onShow} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
      Show Modal
    </button>
  </div>
);

// Create a brand-new custom role: empty name/description, nothing selected.
// Toggle a miner action to watch the read-pairing rule auto-select (and lock)
// miner:read and the fleet:read floor.
export const CreateRole = () => {
  const [show, setShow] = useState(true);

  if (!show) return <ShowAgain onShow={() => setShow(true)} />;

  return (
    <div>
      <Banner>
        Groups start collapsed — expand “Miners” (or search “reboot”), toggle a miner action, and watch{" "}
        <code>miner:read</code> and <code>fleet:read</code> lock on automatically. Click Create role to save.
      </Banner>
      <ToasterMount />
      <CreateEditRoleModal
        onSuccess={() => action("onSuccess")()}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};

// Edit an existing custom role. The story seeds the role into the in-memory
// dataset on mount so Save resolves cleanly, then opens the editor prefilled.
export const EditCustomRole = () => {
  const { createRole } = useRoleManagement();
  const [show, setShow] = useState(true);
  const [role, setRole] = useState<RoleItem | null>(null);

  useEffect(() => {
    createRole({
      name: "Site Operator",
      description: "Day-to-day floor operations: reboot, blink, and pull logs.",
      permissions: ["fleet:read", "miner:read", "miner:reboot", "miner:blink_led", "miner:download_logs"],
      onSuccess: (created) => setRole(created),
    });
  }, [createRole]);

  if (!show || !role) {
    return show ? (
      <div className="p-10 text-text-primary-50">Preparing sample role…</div>
    ) : (
      <ShowAgain onShow={() => setShow(true)} />
    );
  }

  return (
    <div>
      <Banner>Editing a custom role with permissions prefilled. Adjust the selection and click Save changes.</Banner>
      <ToasterMount />
      <CreateEditRoleModal
        role={role}
        onSuccess={() => action("onSuccess")()}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};

// Edit a built-in role (Field Tech). The name is locked and a "Built-in" callout
// explains why; permissions remain editable.
export const EditBuiltInRole = () => {
  const fieldTech: RoleItem = {
    roleId: "builtin-field-tech",
    name: "Field Tech",
    description: "Read fleet data, blink the locator LED, download logs, manage racks.",
    permissions: ["fleet:read", "miner:read", "miner:blink_led", "miner:download_logs", "rack:read", "rack:manage"],
    builtin: true,
    builtinKey: "FIELD_TECH",
    memberCount: 4,
    updatedAt: null,
  };
  const [show, setShow] = useState(true);

  if (!show) return <ShowAgain onShow={() => setShow(true)} />;

  return (
    <div>
      <Banner>Editing the built-in Field Tech role: name is locked, permissions stay editable.</Banner>
      <ToasterMount />
      <CreateEditRoleModal
        role={fieldTech}
        onSuccess={() => action("onSuccess")()}
        onDismiss={() => {
          action("onDismiss")();
          setShow(false);
        }}
      />
    </div>
  );
};
