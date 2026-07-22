import {describe, expect, it} from "vitest";
import {ManagementAPIError, toAPIError} from "./client";

describe("toAPIError", () => {
  it("preserves the stable management error envelope", () => {
    const error = toAPIError(409, {error: {code: "CONFLICT", message: "draft changed", details: {expected: "a"}}});
    expect(error).toBeInstanceOf(ManagementAPIError);
    expect(error).toMatchObject({status: 409, code: "CONFLICT", message: "draft changed", details: {expected: "a"}});
  });

  it("uses a safe fallback without exposing unknown payloads", () => {
    const error = toAPIError(500, "database secret");
    expect(error.message).toBe("Request failed (500).");
    expect(error.message).not.toContain("secret");
  });
});
