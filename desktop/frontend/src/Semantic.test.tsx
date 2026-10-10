import { afterEach, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SemanticSettings, AnalysisPanel } from "./Semantic";
import { visibleItems, type Item, type State } from "./api";
const state: State = {
  config: {
    accountPath: "account.v1",
    storePath: "local.db",
    server: "",
    preferences: { theme: "dark", idleMinutes: 5 },
  },
  unlocked: true,
  generation: 1,
  accountID: "fixture",
  recoveryOnly: false,
  sync: "Local only",
  pending: 0,
  lastSync: "",
};
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});
it("does not start inference or download until explicitly enabled and handles immediate setup completion", async () => {
  let listener: (e: any) => void = () => {};
  window.runtime = {
    EventsOn: (_name, cb) => {
      listener = cb;
      return () => {};
    },
  };
  const setup = vi.fn(async () => {
      listener({ generation: 1, kind: "finished", operation: "setup" });
      return "setup";
    }),
    refresh = vi.fn(async () => {});
  window.go = {
    main: {
      App: {
        SemanticStatus: vi.fn(async () => ({
          enabled: false,
          supported: true,
          ready: false,
          downloadBytes: 113213239,
          thresholds: { cutoff: 0.2, margin: 0 },
        })),
        SetupSemantic: setup,
      } as any,
    },
  };
  render(<SemanticSettings state={state} onRefresh={refresh} />);
  const checkbox = await screen.findByLabelText("Enable on this device");
  expect(checkbox).not.toBeChecked();
  expect(setup).not.toHaveBeenCalled();
  await userEvent.click(checkbox);
  await waitFor(() => expect(refresh).toHaveBeenCalledOnce());
  expect(
    screen.queryByText(/Setting up and indexing locally/),
  ).not.toBeInTheDocument();
});
it("offers replacement model setup for an already enabled klefki", async () => {
  const setup = vi.fn(async () => "bge-setup");
  window.go = { main: { App: {
    SemanticStatus: vi.fn(async () => ({ enabled: true, supported: true, ready: false, downloadBytes: 156146754, thresholds: { cutoff: .5, margin: .01 } })),
    SetupSemantic: setup,
  } as any } };
  render(<SemanticSettings state={state} onRefresh={async () => {}} />);
  expect(await screen.findByLabelText("Enable on this device")).toBeChecked();
  expect(setup).not.toHaveBeenCalled();
  await userEvent.click(screen.getByRole("button", { name: "Install BGE-small model" }));
  await waitFor(() => expect(setup).toHaveBeenCalledOnce());
});
it("shows source evidence and persists confirmed values without submitting document edits", async () => {
  const item: Item = {
    object: "fixture",
    revision: "revision",
    name: "bill.txt",
    type: "file",
    parent: null,
    tags: ["Personal"],
    favorite: false,
    size: 100,
    conflict: false,
    trashed: false,
    trashRoot: false,
  };
  const save = vi.fn(async () => "correction");
  window.go = {
    main: {
      App: {
        Analyze: vi.fn(async () => ({
          generation: 1,
          prediction: {
            category: "Money",
            score: 0.4,
            margin: 0.1,
            method: "zero-shot",
          },
          confirmed: "",
          fields: [
            {
              kind: "amount",
              raw: "INR 123.45",
              value: "123.45",
              currency: "INR",
              evidence: "Paid INR 123.45 for electricity.",
            },
          ],
          conflicts: [],
        })),
        CorrectAnalysis: save,
      } as any,
    },
  };
  render(<AnalysisPanel item={item} />);
  await screen.findByText("Suggested category: Money");
  expect(
    screen.getByText("Paid INR 123.45 for electricity."),
  ).toBeInTheDocument();
  await userEvent.selectOptions(screen.getByLabelText("Category"), "Work");
  const submit = screen.getByRole("button", {
    name: "Confirm category and fields",
  });
  expect(submit).toHaveAttribute("type", "button");
  await userEvent.click(submit);
  expect(save).toHaveBeenCalledWith("revision", "Work", expect.any(Array), []);
  expect(
    visibleItems(
      [{ ...item, category: "Work" }],
      "All documents",
      null,
      "Work",
      "",
      "name",
    ),
  ).toHaveLength(1);
});
