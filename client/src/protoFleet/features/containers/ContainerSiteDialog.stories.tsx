import { useLayoutEffect, useState } from "react";
import { create } from "@bufbuild/protobuf";
import type { Meta, StoryObj } from "@storybook/react";
import ContainerSiteDialog from "./ContainerSiteDialog";
import { ContainerSchema } from "@/protoFleet/api/generated/containers/v1/containers_pb";
import Button from "@/shared/components/Button";

const container = create(ContainerSchema, {
  deviceIdentifier: "ct1-site-example",
  name: "Container 1",
  site: { id: 1n, label: "North" },
});
const meta = {
  title: "Proto Fleet/Containers/Site assignment",
  component: ContainerSiteDialog,
  args: { container, onClose: () => undefined },
  argTypes: { container: { control: false } },
  decorators: [
    (Story, context) => {
      useLayoutEffect(() => {
        const previous = window.fetch;
        window.fetch = async (input, init) => {
          const url = input instanceof Request ? input.url : String(input);
          const respond = (body: unknown) =>
            new Response(JSON.stringify(body), { headers: { "Content-Type": "application/json" } });
          if (url.endsWith("/ListSites"))
            return respond({ sites: [{ site: { id: "1", name: "North" } }, { site: { id: "2", name: "South" } }] });
          if (url.endsWith("/ListContainers"))
            return respond({
              containers: [
                { deviceIdentifier: "ct1-site-example", name: "Container 1", site: { id: "1", label: "North" } },
                { deviceIdentifier: "unassigned-example", name: "Container 2" },
              ],
            });
          if (url.endsWith("/AssignContainersToSite")) {
            if (context.parameters.denied)
              return new Response(
                JSON.stringify({
                  code: "permission_denied",
                  message: JSON.stringify({ required: "site:manage", scope: { site_id: 2 } }),
                }),
                { status: 403, headers: { "Content-Type": "application/json" } },
              );
            return respond({ reassignedCount: "1" });
          }
          return previous(input, init);
        };
        return () => {
          window.fetch = previous;
        };
      }, [context.parameters.denied]);
      return <Story />;
    },
  ],
} satisfies Meta<typeof ContainerSiteDialog>;
export default meta;
type Story = StoryObj<typeof meta>;
export const Assigned: Story = {
  render: function AssignedDialog(args) {
    const [open, setOpen] = useState(true);
    return open ? (
      <ContainerSiteDialog {...args} onClose={() => setOpen(false)} />
    ) : (
      <Button text="Change site" variant="secondary" onClick={() => setOpen(true)} />
    );
  },
};
export const Unassigned: Story = {
  args: { container: create(ContainerSchema, { deviceIdentifier: "unassigned-example", name: "Container 2" }) },
};
export const PermissionDenied: Story = { parameters: { denied: true } };
