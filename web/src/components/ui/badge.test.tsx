import {render, screen} from "@testing-library/react";
import {describe, expect, it} from "vitest";
import {Badge} from "./badge";

describe("Badge", () => {
  it("keeps visible status text in addition to color", () => {
    render(<Badge tone="healthy">Ready</Badge>);
    expect(screen.getByText("Ready")).toBeVisible();
  });
});
