export type MinerbotSuggestionIcon = "activity" | "energy" | "firmware" | "onboarding" | "profitability" | "security";

export type MinerbotSuggestionCard = {
  actionLabel: string;
  description: string;
  icon: MinerbotSuggestionIcon;
  impact: string;
  prompt: string;
  title: string;
};

export const minerbotSuggestionCards: MinerbotSuggestionCard[] = [
  {
    actionLabel: "Forecast failures",
    description: "Review fan, hashboard, PSU, and temperature signals to identify miners likely to fail next.",
    icon: "activity",
    impact: "Reduce downtime",
    prompt: "Forecast failing hardware and recommend the highest-priority repairs.",
    title: "Forecast failing hardware",
  },
  {
    actionLabel: "Tune energy",
    description: "Balance hashrate against power price, curtailment windows, and site-level efficiency.",
    icon: "energy",
    impact: "Protect margin",
    prompt: "Tune the fleet for optimal energy spend based on market conditions.",
    title: "Tune energy spend",
  },
  {
    actionLabel: "Plan updates",
    description: "Find firmware drift, group compatible miners, and prepare a staged update plan.",
    icon: "firmware",
    impact: "Improve efficiency",
    prompt: "Find miners behind firmware and plan a staged update.",
    title: "Keep firmware current",
  },
  {
    actionLabel: "Validate setup",
    description: "Check site, building, rack, pool, and miner configuration before new hardware comes online.",
    icon: "onboarding",
    impact: "Save setup time",
    prompt: "Validate site, building, rack, and miner configuration before launch.",
    title: "Automate onboarding checks",
  },
  {
    actionLabel: "Review passwords",
    description: "Identify weak or shared credentials and prepare a rotation plan that limits downtime.",
    icon: "security",
    impact: "Reduce exposure",
    prompt: "Check weak or shared passwords and recommend a rotation plan.",
    title: "Find password risk",
  },
  {
    actionLabel: "Build report",
    description: "Summarize hashprice, downtime, power cost, pool performance, and site contribution.",
    icon: "profitability",
    impact: "Drive profitability",
    prompt: "Generate a P&L report with the top operational levers.",
    title: "Generate P&L report",
  },
];
