import {render, screen} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {describe, expect, it, vi} from "vitest";
import {AuthPage} from "./auth-page";

describe("AuthPage", () => {
  it("submits the one-time bootstrap credentials", async () => {
    const user = userEvent.setup();
    const submit = vi.fn().mockResolvedValue(undefined);
    render(<AuthPage mode="bootstrap" onSubmit={submit} />);
    expect(screen.getByRole("heading", {name: "Claim this gateway"})).toBeInTheDocument();
    await user.clear(screen.getByLabelText("Username"));
    await user.type(screen.getByLabelText("Username"), "operator");
    await user.type(screen.getByLabelText(/New password/), "long-enough-password");
    await user.click(screen.getByRole("button", {name: "Create administrator"}));
    expect(submit).toHaveBeenCalledWith("operator", "long-enough-password");
  });

  it("shows a safe login failure", async () => {
    const user = userEvent.setup();
    const submit = vi.fn().mockRejectedValue(new Error("internal detail"));
    render(<AuthPage mode="login" onSubmit={submit} />);
    await user.type(screen.getByLabelText("Password"), "bad-password");
    await user.click(screen.getByRole("button", {name: "Enter signal desk"}));
    expect(await screen.findByRole("alert")).toHaveTextContent("Authentication failed");
    expect(screen.getByRole("alert")).not.toHaveTextContent("internal detail");
  });
});
