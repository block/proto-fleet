import { useState } from "react";
import { action } from "storybook/actions";
import DeleteRoleDialog from "./DeleteRoleDialog";

export default {
  title: "Proto Fleet/Settings/DeleteRoleDialog",
  component: DeleteRoleDialog,
};

const ShowAgain = ({ onShow }: { onShow: () => void }) => (
  <div className="flex h-screen items-center justify-center">
    <button onClick={onShow} className="bg-emphasis-300 rounded-lg px-4 py-2 text-surface-base">
      Show Dialog
    </button>
  </div>
);

// Deletable custom role with no members assigned.
export const Default = () => {
  const [show, setShow] = useState(true);

  if (!show) return <ShowAgain onShow={() => setShow(true)} />;

  return (
    <DeleteRoleDialog
      roleName="Site Operator"
      memberCount={0}
      onConfirm={() => {
        action("onConfirm")();
        setShow(false);
      }}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={false}
    />
  );
};

// Role still assigned to members — delete is blocked and the copy explains why.
export const BlockedByMembers = () => {
  const [show, setShow] = useState(true);

  if (!show) return <ShowAgain onShow={() => setShow(true)} />;

  return (
    <DeleteRoleDialog
      roleName="Site Operator"
      memberCount={5}
      onConfirm={() => action("onConfirm")()}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={false}
    />
  );
};

// Single-member wording variant ("them" / "member").
export const BlockedBySingleMember = () => {
  const [show, setShow] = useState(true);

  if (!show) return <ShowAgain onShow={() => setShow(true)} />;

  return (
    <DeleteRoleDialog
      roleName="Auditor"
      memberCount={1}
      onConfirm={() => action("onConfirm")()}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={false}
    />
  );
};

// Deletion in flight.
export const Loading = () => {
  const [show, setShow] = useState(true);

  if (!show) return <ShowAgain onShow={() => setShow(true)} />;

  return (
    <DeleteRoleDialog
      roleName="Site Operator"
      memberCount={0}
      onConfirm={() => action("onConfirm")()}
      onDismiss={() => {
        action("onDismiss")();
        setShow(false);
      }}
      isSubmitting={true}
    />
  );
};
