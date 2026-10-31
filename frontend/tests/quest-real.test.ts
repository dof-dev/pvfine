import { readFileSync } from "node:fs";
import { expect, test } from "vitest";
import { editQuest, parseQuest } from "../src/gui/quest";

const corpus = process.env.PVF_GUI_QUEST_CORPUS;

test.skipIf(!corpus)("real archive quests preserve source ranges and support integer edits", () => {
  const fixtures: { text: string }[] = JSON.parse(readFileSync(corpus!, "utf8"));
  expect(fixtures.length).toBeGreaterThan(0);
  let failed = 0, issues = 0, edited = 0;
  const errors = new Map<string, number>();
  for (const { text } of fixtures) {
    let doc: ReturnType<typeof parseQuest>;
    try {
      doc = parseQuest(text);
    } catch (cause) {
      failed++;
      const message = cause instanceof Error ? cause.message.split("\n")[0]! : String(cause);
      errors.set(message, (errors.get(message) ?? 0) + 1);
      continue;
    }
    if (doc.sections.some(s => s.issue)) issues++;
    expect(editQuest(text, [])).toBe(text);
    const field = doc.sections.find(s => !s.issue && s.tokens.length > 0 && s.tokens.every(t => t.kind === "integer"));
    if (!field) continue;
    const values = field.tokens.map(t => t.value);
    const first = field.tokens[0]!;
    const next = first.value === "0" ? "1" : "0";
    values[0] = next;
    expect(editQuest(text, [{ name: field.name, kind: "integer", values }])).toBe(
      text.slice(0, first.start) + next + text.slice(first.end),
    );
    edited++;
  }
  console.log(JSON.stringify({ quests: fixtures.length, failed, issues, edited, errors: Object.fromEntries(errors) }));
  expect(failed).toBe(Number(process.env.PVF_GUI_QUEST_EXPECTED_REJECTIONS ?? 0));
  expect(edited).toBeGreaterThan(0);
}, 120_000);
