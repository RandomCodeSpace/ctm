import { describe, it, expect } from "vitest";
import { ApiError } from "@/lib/api";
import { serverMessage } from "./serverMessage";

describe("serverMessage", () => {
  it("returns the body.message when ApiError carries a string message", () => {
    const err = new ApiError(400, "bad request", { message: "username already taken" });
    expect(serverMessage(err)).toBe("username already taken");
  });

  it("returns undefined when the error is not an ApiError", () => {
    expect(serverMessage(new Error("network"))).toBeUndefined();
    expect(serverMessage("oops")).toBeUndefined();
    expect(serverMessage(null)).toBeUndefined();
  });

  it("returns undefined when the body has no message field", () => {
    const err = new ApiError(500, "internal", { code: 42 });
    expect(serverMessage(err)).toBeUndefined();
  });

  it("returns undefined when message is non-string", () => {
    const err = new ApiError(400, "bad", { message: 123 });
    expect(serverMessage(err)).toBeUndefined();
  });

  it("returns undefined when body is null or non-object", () => {
    expect(serverMessage(new ApiError(400, "bad", null))).toBeUndefined();
    expect(serverMessage(new ApiError(400, "bad", "string body"))).toBeUndefined();
  });
});
