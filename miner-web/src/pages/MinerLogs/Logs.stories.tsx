import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import { mockLogs } from "./constants";
import LogComponent from "./Logs";
import { formatLogs } from "./utility";

export const Logs = () => {
  return <LogComponent logs={formatLogs(mockLogs.content)} />;
};

export default {
  title: "Components/Logs",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
