import Roles from "./Roles";
import { useFleetStore } from "@/protoFleet/store";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export default {
  title: "Proto Fleet/Settings/Roles",
  component: Roles,
};

// The page is gated on the role:manage permission (it redirects without it).
// Seed the store at module load so the page renders in isolation here.
useFleetStore.getState().auth.setPermissions(["role:manage"]);

// Full roles management surface: the three built-in roles plus any custom roles
// created during the session. Use "Create role" / the row actions to open the
// CreateEditRoleModal and DeleteRoleDialog in place.
export const Default = () => (
  <div className="p-10">
    <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
      <ToasterComponent />
    </div>
    <Roles />
  </div>
);
