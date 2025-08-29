import { statuses } from "@/shared/components/StatusCircle";

export type Issue = {
  title?: string;
  message?: string;
  details?: string;
};

export type MinerStatus = {
  circle: keyof typeof statuses;
  summary?: string;
  title: string;
  subtitle?: string;
  hasIssues: boolean;
  isSleeping?: boolean;
  isMining?: boolean;
  issues: {
    hashboards: Issue[];
    psus: Issue[];
    fans: Issue[];
    controlBoard: Issue[];
  };
};
