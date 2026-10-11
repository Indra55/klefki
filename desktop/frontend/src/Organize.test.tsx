import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Organize } from "./Organize";
import type { OrganizationPlan } from "./api";

const plan: OrganizationPlan = {
  generation: 1, skipped: 1, tags: ["Money", "Jazz Music"], suggestions: [
    { revision: "invoice", name: "invoice.txt", tags: ["Personal"], tag: "Money", new: false, basis: "matching tag", score: .75 },
    { revision: "concert", name: "concert.txt", tags: [], tag: "Jazz Music", new: true, basis: "new content group", score: .9 },
    { revision: "rehearsal", name: "rehearsal.txt", tags: [], tag: "Jazz Music", new: true, basis: "new content group", score: .9 },
  ],
};

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

function fixture(result = plan) {
  const suggest = vi.fn(async (_custom: string[]) => result);
  const apply = vi.fn(async (_generation: number, _changes: unknown[]) => 2);
  const onApplied = vi.fn(async (_count: number) => {});
  window.go = { main: { App: { SuggestOrganization: suggest, ApplyOrganization: apply } as any } };
  render(<Organize generation={1} onClose={() => {}} onApplied={onApplied} />);
  return { suggest, apply, onApplied };
}

it("only previews on request and applies reviewed tags with group renaming and exclusions", async () => {
  const { suggest, apply, onApplied } = fixture();
  expect(suggest).not.toHaveBeenCalled();
  await userEvent.type(screen.getByLabelText("Custom tags to match"), "Recipes, Travel, Recipes");
  await userEvent.click(screen.getByRole("button", { name: "Find tags" }));
  expect(suggest).toHaveBeenCalledWith(["Recipes", "Travel"]);
  const input = await screen.findByLabelText("Tag for Jazz Music");
  expect(screen.getByText("Keeping: Personal")).toBeInTheDocument();
  expect(apply).not.toHaveBeenCalled();
  await userEvent.clear(input);
  await userEvent.type(input, "Music");
  await userEvent.click(screen.getByRole("checkbox", { name: /^invoice\.txt/ }));
  await userEvent.click(screen.getByRole("button", { name: "Apply tags to 2 files" }));
  expect(apply).toHaveBeenCalledWith(1, [
    { revision: "concert", tag: "Music" }, { revision: "rehearsal", tag: "Music" },
  ]);
  expect(onApplied).toHaveBeenCalledWith(2);
});

it("requires a selected file and a nonempty reviewed tag", async () => {
  fixture();
  await userEvent.click(screen.getByRole("button", { name: "Find tags" }));
  await screen.findByLabelText("Tag for Money");
  await userEvent.click(screen.getByRole("button", { name: "Deselect all" }));
  expect(screen.getByRole("button", { name: "Apply tags to 0 files" })).toBeDisabled();
  await userEvent.click(screen.getByRole("button", { name: "Select all" }));
  await userEvent.clear(screen.getByLabelText("Tag for Money"));
  expect(screen.getByRole("button", { name: "Apply tags to 3 files" })).toBeDisabled();
});

it("rejects a preview from another unlocked session", async () => {
  const { apply } = fixture({ ...plan, generation: 2 });
  await userEvent.click(screen.getByRole("button", { name: "Find tags" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("This document changed");
  expect(screen.queryByLabelText("Tag for Money")).not.toBeInTheDocument();
  expect(apply).not.toHaveBeenCalled();
});

it("keeps the review available after a stale batch fails", async () => {
  const { apply, onApplied } = fixture();
  apply.mockRejectedValueOnce(new Error("STALE"));
  await userEvent.click(screen.getByRole("button", { name: "Find tags" }));
  await userEvent.click(await screen.findByRole("button", { name: "Apply tags to 3 files" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("This document changed");
  expect(screen.getByLabelText("Tag for Jazz Music")).toHaveValue("Jazz Music");
  expect(onApplied).not.toHaveBeenCalled();
});

it("discards preview results after the private screen is unmounted", async () => {
  let resolve!: (plan: OrganizationPlan) => void;
  const pending = new Promise<OrganizationPlan>(r => { resolve = r; });
  const apply = vi.fn();
  window.go = { main: { App: { SuggestOrganization: vi.fn(() => pending), ApplyOrganization: apply } as any } };
  const view = render(<Organize generation={1} onClose={() => {}} onApplied={async () => {}} />);
  await userEvent.click(screen.getByRole("button", { name: "Find tags" }));
  view.unmount();
  resolve(plan);
  await waitFor(() => expect(screen.queryByText("invoice.txt")).not.toBeInTheDocument());
  expect(apply).not.toHaveBeenCalled();
});
