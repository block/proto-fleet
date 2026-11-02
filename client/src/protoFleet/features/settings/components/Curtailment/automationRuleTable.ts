// Keeps the trailing "enabled" switch column a fixed width on phones.
export const automationRuleTableClassName = [
  "mb-2 w-full",
  "phone:table-fixed",
  "phone:[&_thead_th:last-child]:w-14",
  "phone:[&_thead_th:last-child>div]:w-14",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:box-border",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:flex",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:justify-end",
  "phone:[&_tbody_td[data-testid=enabled]:last-child>div:first-child]:w-14",
].join(" ");
