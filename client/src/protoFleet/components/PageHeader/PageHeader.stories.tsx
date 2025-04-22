import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import PageHeaderComponent from ".";

export const PageHeader = () => {
  return <PageHeaderComponent />;
};

export default {
  title: "Components (protoFleet)/Page Header",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
