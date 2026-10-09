import type { ComponentProps } from "react";
import MiningActionsMenu from "./MiningActionsMenu";
export default function ModuleActionsMenu({
  modules,
  ...props
}: Omit<ComponentProps<typeof MiningActionsMenu>, "kind" | "items"> & {
  modules: ComponentProps<typeof MiningActionsMenu>["items"];
}) {
  return <MiningActionsMenu {...props} items={modules} kind="module" />;
}
