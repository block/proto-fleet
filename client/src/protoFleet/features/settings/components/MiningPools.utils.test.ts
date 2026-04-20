import { describe, expect, it } from "vitest";

// Import the functions from MiningPools.tsx
// Note: These functions are not exported, so we'll redefine them here for testing
// In a real scenario, you'd want to export these from a separate utils file

// Add zero-width spaces at good break points in URLs
const formatUrlForWrapping = (url: string) => {
  return url
    .replace(/(:\/\/)/g, "$1\u200B") // After ://
    .replace(/(\/)/g, "$1\u200B") // After /
    .replace(/(\.)(?=\w)/g, "$1\u200B") // After . followed by word char
    .replace(/(:)(?=\d)/g, "$1\u200B") // After : followed by digit (port)
    .replace(/(\+)/g, "$1\u200B"); // After +
};

// Add zero-width spaces at good break points in usernames
const formatUsernameForWrapping = (username: string) => {
  return username
    .replace(/(_)/g, "$1\u200B") // After underscore
    .replace(/(\.)(?=\w)/g, "$1\u200B") // After . followed by word char
    .replace(/(-)/g, "$1\u200B"); // After hyphen
};

describe("formatUrlForWrapping", () => {
  it("inserts zero-width space after protocol separator", () => {
    const result = formatUrlForWrapping("stratum+tcp://test.com:3333");
    // Note: '://' gets one space after, then each '/' gets another space (3 total)
    expect(result).toBe("stratum+\u200Btcp:/\u200B/\u200B\u200Btest.\u200Bcom:\u200B3333");
  });

  it("inserts zero-width space after forward slashes", () => {
    const result = formatUrlForWrapping("http://example.com/path/to/resource");
    // Note: '://' gets 3 spaces total, other '/' get 1 space each
    expect(result).toBe("http:/\u200B/\u200B\u200Bexample.\u200Bcom/\u200Bpath/\u200Bto/\u200Bresource");
  });

  it("inserts zero-width space after dots followed by word characters", () => {
    const result = formatUrlForWrapping("test.example.com");
    expect(result).toBe("test.\u200Bexample.\u200Bcom");
  });

  it("inserts zero-width space after colon followed by port number", () => {
    const result = formatUrlForWrapping("example.com:8080");
    expect(result).toBe("example.\u200Bcom:\u200B8080");
  });

  it("inserts zero-width space after plus signs", () => {
    const result = formatUrlForWrapping("stratum+tcp://pool.com");
    expect(result).toBe("stratum+\u200Btcp:/\u200B/\u200B\u200Bpool.\u200Bcom");
  });

  it("handles URLs without protocol", () => {
    const result = formatUrlForWrapping("example.com:3333");
    expect(result).toBe("example.\u200Bcom:\u200B3333");
  });

  it("handles URLs with multiple consecutive special characters", () => {
    const result = formatUrlForWrapping("http://test...com");
    // '://' gets 3 spaces, dots not followed by word chars don't get spaces
    expect(result).toBe("http:/\u200B/\u200B\u200Btest...\u200Bcom");
  });

  it("handles empty strings", () => {
    const result = formatUrlForWrapping("");
    expect(result).toBe("");
  });

  it("handles complex mining pool URLs", () => {
    const result = formatUrlForWrapping("stratum+tcp://pool.example.com:3333/worker");
    expect(result).toBe("stratum+\u200Btcp:/\u200B/\u200B\u200Bpool.\u200Bexample.\u200Bcom:\u200B3333/\u200Bworker");
  });
});

describe("formatUsernameForWrapping", () => {
  it("inserts zero-width space after underscores", () => {
    const result = formatUsernameForWrapping("user_name_123");
    expect(result).toBe("user_\u200Bname_\u200B123");
  });

  it("inserts zero-width space after dots followed by word characters", () => {
    const result = formatUsernameForWrapping("user.name.worker");
    expect(result).toBe("user.\u200Bname.\u200Bworker");
  });

  it("inserts zero-width space after hyphens", () => {
    const result = formatUsernameForWrapping("user-name-123");
    expect(result).toBe("user-\u200Bname-\u200B123");
  });

  it("handles usernames with multiple special characters", () => {
    const result = formatUsernameForWrapping("user_name-123.worker");
    expect(result).toBe("user_\u200Bname-\u200B123.\u200Bworker");
  });

  it("handles empty strings", () => {
    const result = formatUsernameForWrapping("");
    expect(result).toBe("");
  });

  it("handles usernames with consecutive underscores", () => {
    const result = formatUsernameForWrapping("user__name");
    expect(result).toBe("user_\u200B_\u200Bname");
  });

  it("handles simple usernames without special characters", () => {
    const result = formatUsernameForWrapping("username123");
    expect(result).toBe("username123");
  });

  it("handles complex mining pool usernames", () => {
    const result = formatUsernameForWrapping("wallet_address.worker-01");
    expect(result).toBe("wallet_\u200Baddress.\u200Bworker-\u200B01");
  });
});
