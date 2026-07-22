import type { ChatSuggestion } from "./types";

type ChatContextKey =
  | "dashboard"
  | "minerbot"
  | "fleetMiners"
  | "fleetRacks"
  | "fleetSites"
  | "fleetInfrastructure"
  | "groups"
  | "energy"
  | "activity"
  | "singleMiner"
  | "settings"
  | "settingsAgents"
  | "settingsFirmware"
  | "settingsSchedules"
  | "settingsCurtailment"
  | "onboarding";

export interface ChatContext {
  key: ChatContextKey;
  description: string;
  suggestions: ChatSuggestion[];
}

const chatContexts: Record<ChatContextKey, ChatContext> = {
  dashboard: {
    key: "dashboard",
    description: "Ask about fleet health, profitability, and urgent risks.",
    suggestions: [
      { label: "Summarize fleet health" },
      { label: "Find profit risks today" },
      { label: "What needs attention first?" },
    ],
  },
  minerbot: {
    key: "minerbot",
    description: "Ask Minerbot to review fleet signals, prepare actions, or plan recurring work.",
    suggestions: [
      { label: "Start a fleet health review" },
      { label: "Plan recurring work" },
      { label: "Find tasks to automate" },
    ],
  },
  fleetMiners: {
    key: "fleetMiners",
    description: "Ask about miner status, firmware drift, and security risks.",
    suggestions: [
      { label: "Which miners are offline?" },
      { label: "Find firmware drift" },
      { label: "Check weak or shared passwords" },
    ],
  },
  fleetRacks: {
    key: "fleetRacks",
    description: "Ask about rack health, placement, and underperforming slots.",
    suggestions: [
      { label: "Find rack hotspots" },
      { label: "Show underperforming slots" },
      { label: "Suggest miner placement fixes" },
    ],
  },
  fleetSites: {
    key: "fleetSites",
    description: "Ask about site setup, building health, and rack configuration.",
    suggestions: [
      { label: "Review site setup" },
      { label: "Find building health risks" },
      { label: "Suggest rack configuration fixes" },
    ],
  },
  fleetInfrastructure: {
    key: "fleetInfrastructure",
    description: "Ask about infrastructure health, network risks, and devices needing attention.",
    suggestions: [
      { label: "Summarize infrastructure health" },
      { label: "Find network risks" },
      { label: "Review devices needing attention" },
    ],
  },
  groups: {
    key: "groups",
    description: "Ask about group performance, outliers, and group-level actions.",
    suggestions: [
      { label: "Compare group performance" },
      { label: "Find underperforming groups" },
      { label: "Suggest group-level actions" },
    ],
  },
  energy: {
    key: "energy",
    description: "Ask about curtailment impact, power pricing, and market-aware operations.",
    suggestions: [
      { label: "Summarize curtailment impact" },
      { label: "Find profitable power changes" },
      { label: "Plan today's power strategy" },
    ],
  },
  activity: {
    key: "activity",
    description: "Ask about recent changes, incomplete operations, and operator activity.",
    suggestions: [
      { label: "Summarize recent activity" },
      { label: "Find operations needing review" },
      { label: "What changed today?" },
    ],
  },
  singleMiner: {
    key: "singleMiner",
    description: "Ask about this miner's health, hashrate, and safe next actions.",
    suggestions: [
      { label: "Summarize this miner" },
      { label: "Diagnose hashrate drop" },
      { label: "Review safe actions" },
    ],
  },
  settings: {
    key: "settings",
    description: "Ask about setup gaps, operating defaults, and recurring tasks.",
    suggestions: [
      { label: "Review setup gaps" },
      { label: "Suggest operational defaults" },
      { label: "Show recurring tasks to automate" },
    ],
  },
  settingsAgents: {
    key: "settingsAgents",
    description: "Ask about Minerbot setup, provider choices, and available capabilities.",
    suggestions: [
      { label: "Check Minerbot setup" },
      { label: "Help choose a provider" },
      { label: "What can Minerbot do here?" },
    ],
  },
  settingsFirmware: {
    key: "settingsFirmware",
    description: "Ask about firmware defaults, version drift, and staged updates.",
    suggestions: [
      { label: "Review firmware defaults" },
      { label: "Find miners behind firmware" },
      { label: "Plan a staged update" },
    ],
  },
  settingsSchedules: {
    key: "settingsSchedules",
    description: "Ask about schedules, conflicts, and recurring operating tasks.",
    suggestions: [
      { label: "Review power schedules" },
      { label: "Find schedule conflicts" },
      { label: "Suggest recurring tasks" },
    ],
  },
  settingsCurtailment: {
    key: "settingsCurtailment",
    description: "Ask about curtailment settings, market windows, and power rules.",
    suggestions: [
      { label: "Review curtailment settings" },
      { label: "Find profitable curtailment windows" },
      { label: "Suggest market-based rules" },
    ],
  },
  onboarding: {
    key: "onboarding",
    description: "Ask about site setup, miner configuration, and launch checks.",
    suggestions: [
      { label: "Plan site setup" },
      { label: "Validate miner configuration" },
      { label: "What should I check before launch?" },
    ],
  },
};

const SCOPABLE_ROUTE_SEGMENTS = new Set(["dashboard", "fleet", "groups", "energy", "activity"]);

const pathSegments = (pathname: string) =>
  pathname
    .split("/")
    .map((segment) => segment.trim())
    .filter(Boolean);

const normalizedSegments = (pathname: string) => {
  const segments = pathSegments(pathname);
  return segments.length > 1 && SCOPABLE_ROUTE_SEGMENTS.has(segments[1]) ? segments.slice(1) : segments;
};

export const getChatContext = (pathname: string): ChatContext => {
  const [primary, secondary] = normalizedSegments(pathname);

  if (!primary || primary === "dashboard") return chatContexts.dashboard;
  if (primary === "minerbot") return chatContexts.minerbot;
  if (primary === "groups") return chatContexts.groups;
  if (primary === "energy") return chatContexts.energy;
  if (primary === "activity") return chatContexts.activity;
  if (primary === "miners") return chatContexts.singleMiner;
  if (primary === "racks") return chatContexts.fleetRacks;
  if (primary === "sites" || primary === "buildings") return chatContexts.fleetSites;
  if (primary === "onboarding") return chatContexts.onboarding;

  if (primary === "fleet") {
    if (secondary === "miners") return chatContexts.fleetMiners;
    if (secondary === "racks") return chatContexts.fleetRacks;
    if (secondary === "sites" || secondary === "buildings") return chatContexts.fleetSites;
    if (secondary === "infrastructure") return chatContexts.fleetInfrastructure;
    return chatContexts.fleetMiners;
  }

  if (primary === "settings") {
    if (secondary === "agents") return chatContexts.settingsAgents;
    if (secondary === "firmware") return chatContexts.settingsFirmware;
    if (secondary === "schedules") return chatContexts.settingsSchedules;
    if (secondary === "curtailment") return chatContexts.settingsCurtailment;
    return chatContexts.settings;
  }

  return chatContexts.dashboard;
};
