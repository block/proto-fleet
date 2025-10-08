import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import PageHeaderComponent from ".";

export const PageHeader = () => {
  return <PageHeaderComponent />;
};

export default {
  title: "Proto Fleet/Page Header",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
