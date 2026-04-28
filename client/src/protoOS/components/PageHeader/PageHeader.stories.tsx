import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import PageHeaderComponent from ".";

export const PageHeader = () => {
  return <PageHeaderComponent title="Title" />;
};

export default {
  title: "Proto OS/Page Header",
  parameters: {
    withRouter: false,
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
