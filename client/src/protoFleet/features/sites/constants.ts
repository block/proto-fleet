// US state options for the SiteDetailsModal state dropdown. Values are the
// USPS two-letter codes — matches what the backend stores (no canonicalization
// pass) and what users expect to see populated when editing.
export const US_STATE_OPTIONS = [
  { value: "AL", label: "Alabama" },
  { value: "AK", label: "Alaska" },
  { value: "AZ", label: "Arizona" },
  { value: "AR", label: "Arkansas" },
  { value: "CA", label: "California" },
  { value: "CO", label: "Colorado" },
  { value: "CT", label: "Connecticut" },
  { value: "DE", label: "Delaware" },
  { value: "DC", label: "District of Columbia" },
  { value: "FL", label: "Florida" },
  { value: "GA", label: "Georgia" },
  { value: "HI", label: "Hawaii" },
  { value: "ID", label: "Idaho" },
  { value: "IL", label: "Illinois" },
  { value: "IN", label: "Indiana" },
  { value: "IA", label: "Iowa" },
  { value: "KS", label: "Kansas" },
  { value: "KY", label: "Kentucky" },
  { value: "LA", label: "Louisiana" },
  { value: "ME", label: "Maine" },
  { value: "MD", label: "Maryland" },
  { value: "MA", label: "Massachusetts" },
  { value: "MI", label: "Michigan" },
  { value: "MN", label: "Minnesota" },
  { value: "MS", label: "Mississippi" },
  { value: "MO", label: "Missouri" },
  { value: "MT", label: "Montana" },
  { value: "NE", label: "Nebraska" },
  { value: "NV", label: "Nevada" },
  { value: "NH", label: "New Hampshire" },
  { value: "NJ", label: "New Jersey" },
  { value: "NM", label: "New Mexico" },
  { value: "NY", label: "New York" },
  { value: "NC", label: "North Carolina" },
  { value: "ND", label: "North Dakota" },
  { value: "OH", label: "Ohio" },
  { value: "OK", label: "Oklahoma" },
  { value: "OR", label: "Oregon" },
  { value: "PA", label: "Pennsylvania" },
  { value: "RI", label: "Rhode Island" },
  { value: "SC", label: "South Carolina" },
  { value: "SD", label: "South Dakota" },
  { value: "TN", label: "Tennessee" },
  { value: "TX", label: "Texas" },
  { value: "UT", label: "Utah" },
  { value: "VT", label: "Vermont" },
  { value: "VA", label: "Virginia" },
  { value: "WA", label: "Washington" },
  { value: "WV", label: "West Virginia" },
  { value: "WI", label: "Wisconsin" },
  { value: "WY", label: "Wyoming" },
];

// Canadian province / territory two-letter codes. Matches the server's
// caProvinceToTimezone lookup; both lists must stay in sync.
export const CA_PROVINCE_OPTIONS = [
  { value: "AB", label: "Alberta" },
  { value: "BC", label: "British Columbia" },
  { value: "MB", label: "Manitoba" },
  { value: "NB", label: "New Brunswick" },
  { value: "NL", label: "Newfoundland and Labrador" },
  { value: "NS", label: "Nova Scotia" },
  { value: "NT", label: "Northwest Territories" },
  { value: "NU", label: "Nunavut" },
  { value: "ON", label: "Ontario" },
  { value: "PE", label: "Prince Edward Island" },
  { value: "QC", label: "Quebec" },
  { value: "SK", label: "Saskatchewan" },
  { value: "YT", label: "Yukon" },
];

// Country options for the SiteSettingsModal country dropdown. Values
// are ISO 3166-1 alpha-2 codes. Only countries with matching state /
// timezone tables appear here.
export const COUNTRY_OPTIONS = [
  { value: "US", label: "United States" },
  { value: "CA", label: "Canada" },
];

// IANA timezone options for the SiteSettingsModal timezone Select.
// Superset of the ids returned by inferTimezone so operators can
// override a minority-zone state (e.g. ID → America/Los_Angeles for
// the panhandle). Labels lead with the offset so a manual override
// is easy to verify.
export const TIMEZONE_OPTIONS = [
  // US
  { value: "America/New_York", label: "Eastern Time — New York" },
  { value: "America/Detroit", label: "Eastern Time — Detroit" },
  { value: "America/Indiana/Indianapolis", label: "Eastern Time — Indianapolis" },
  { value: "America/Chicago", label: "Central Time — Chicago" },
  { value: "America/Denver", label: "Mountain Time — Denver" },
  { value: "America/Boise", label: "Mountain Time — Boise" },
  { value: "America/Phoenix", label: "Mountain Time (no DST) — Phoenix" },
  { value: "America/Los_Angeles", label: "Pacific Time — Los Angeles" },
  { value: "America/Anchorage", label: "Alaska Time — Anchorage" },
  { value: "Pacific/Honolulu", label: "Hawaii Time — Honolulu" },
  // CA
  { value: "America/St_Johns", label: "Newfoundland Time — St. John's" },
  { value: "America/Halifax", label: "Atlantic Time — Halifax" },
  { value: "America/Moncton", label: "Atlantic Time — Moncton" },
  { value: "America/Toronto", label: "Eastern Time — Toronto" },
  { value: "America/Iqaluit", label: "Eastern Time — Iqaluit" },
  { value: "America/Winnipeg", label: "Central Time — Winnipeg" },
  { value: "America/Regina", label: "Central Time (no DST) — Regina" },
  { value: "America/Edmonton", label: "Mountain Time — Edmonton" },
  { value: "America/Yellowknife", label: "Mountain Time — Yellowknife" },
  { value: "America/Vancouver", label: "Pacific Time — Vancouver" },
  { value: "America/Whitehorse", label: "Yukon Time — Whitehorse" },
];
