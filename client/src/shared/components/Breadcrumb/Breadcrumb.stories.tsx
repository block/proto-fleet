import type { Meta, StoryObj } from "@storybook/react";

import Breadcrumb from "./Breadcrumb";
import type { BreadcrumbProps } from "./Breadcrumb";

const meta: Meta<BreadcrumbProps> = {
  title: "Shared/Breadcrumb",
  component: Breadcrumb,
  parameters: {
    layout: "padded",
    docs: {
      description: {
        component:
          "Hierarchical breadcrumb navigation. Ancestor segments render as links; the last segment can include a sibling switcher dropdown.",
      },
    },
  },
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<BreadcrumbProps>;

export const SiteLevel: Story = {
  name: "Site (single level with switcher)",
  args: {
    segments: [
      {
        label: "Denver",
        siblings: [
          { label: "Reno", to: "/sites/1", isActive: false },
          { label: "Austin", to: "/sites/2", isActive: false },
          { label: "Denver", to: "/sites/3", isActive: true },
          { label: "Miami", to: "/sites/4", isActive: false },
          { label: "Marfa", to: "/sites/5", isActive: false },
        ],
      },
    ],
  },
};

export const BuildingLevel: Story = {
  name: "Building (site link + building switcher)",
  args: {
    segments: [
      { label: "Denver", to: "/sites/3" },
      {
        label: "Building 3",
        siblings: [
          { label: "Building 1", to: "/buildings/1", isActive: false },
          { label: "Building 2", to: "/buildings/2", isActive: false },
          { label: "Building 3", to: "/buildings/3", isActive: true },
          { label: "Building 4", to: "/buildings/4", isActive: false },
        ],
      },
    ],
  },
};

export const RackLevel: Story = {
  name: "Rack (site + building links, rack switcher)",
  args: {
    segments: [
      { label: "Denver", to: "/sites/3" },
      { label: "Building 3", to: "/buildings/3" },
      {
        label: "R05",
        siblings: [
          { label: "R01", to: "/racks/1", isActive: false },
          { label: "R02", to: "/racks/2", isActive: false },
          { label: "R03", to: "/racks/3", isActive: false },
          { label: "R04", to: "/racks/4", isActive: false },
          { label: "R05", to: "/racks/5", isActive: true },
          { label: "R06", to: "/racks/6", isActive: false },
        ],
      },
    ],
  },
};

export const NoSiblings: Story = {
  name: "No sibling switcher",
  args: {
    segments: [{ label: "Denver", to: "/sites/3" }, { label: "Building 3" }],
  },
};
