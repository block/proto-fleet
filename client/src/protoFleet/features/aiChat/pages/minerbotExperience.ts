import type { ChatTranscriptTurn } from "../types";

export type MinerbotSuggestionIcon = "activity" | "energy" | "firmware" | "onboarding" | "profitability" | "security";

export type MinerbotSuggestionCard = {
  actionLabel: string;
  description: string;
  icon: MinerbotSuggestionIcon;
  impact: string;
  prompt: string;
  title: string;
};

export type MinerbotHistoryThread = {
  id: string;
  messages: ChatTranscriptTurn[];
  timeLabel: string;
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

export const minerbotHistoryThreads: MinerbotHistoryThread[] = [
  {
    id: "firmware-drift-review",
    timeLabel: "2 hours ago",
    title: "Firmware drift review",
    messages: [
      { role: "user", content: "Find miners behind firmware and plan a staged update." },
      {
        role: "assistant",
        content:
          "I found 8 miners behind the recommended firmware. Start with Rack R03 because its stale share rate is 4.8% above fleet median.\n\n| Group | Miners | Risk | Recommended action |\n| --- | ---: | --- | --- |\n| Rack R03 | 5 | High stale shares | Stage update after current payout window |\n| Rack R07 | 3 | Efficiency drift | Queue after R03 is stable |\n\nI would update in two batches and keep a rollback window open for 30 minutes after each batch.",
      },
    ],
  },
  {
    id: "power-strategy",
    timeLabel: "Yesterday",
    title: "Power strategy",
    messages: [
      { role: "user", content: "Tune the fleet for optimal energy spend based on market conditions." },
      {
        role: "assistant",
        content:
          "Austin can preserve margin by reducing draw during the next high-price window and returning full hashrate when prices normalize.\n\n| Site | Window | Suggested action | Expected effect |\n| --- | --- | --- | --- |\n| Austin | 2:00 PM-5:00 PM | Curtail lowest-efficiency miners | Avoid negative-margin hash |\n| Tulsa | 6:00 PM-8:00 PM | Hold current profile | Stable margin |\n\nBefore applying this, I would ask for operator approval because it changes power state.",
      },
    ],
  },
  {
    id: "password-risk-review",
    timeLabel: "Monday",
    title: "Password risk review",
    messages: [
      { role: "user", content: "Check weak or shared passwords and recommend a rotation plan." },
      {
        role: "assistant",
        content:
          "I found 17 miners with shared credentials across 3 racks. Rotate Rack A7 first because it has the largest overlap with externally reachable network paths.\n\n| Scope | Miners | Issue | Next step |\n| --- | ---: | --- | --- |\n| Rack A7 | 9 | Shared password | Rotate during maintenance window |\n| Rack C2 | 5 | Weak password | Queue credential update |\n| Rack D4 | 3 | Default username | Review access policy |\n\nI can prepare the rotation set, but should request confirmation before changing credentials.",
      },
    ],
  },
];
