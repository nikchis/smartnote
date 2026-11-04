// @vitest-environment happy-dom
import { afterEach, describe, expect, it } from "vitest";
import type { SearchHitDTO } from "./api";
import { scrollTopFor, textMatches } from "./jump";

function hit(line: number, start: number, end: number, text: string): SearchHitDTO {
  return { noteId: "a", line, start, end, text, snippet: text, marks: [], sources: [] };
}

describe("textMatches", () => {
  const name = "Shopping list";
  const info = "milk\nbread\neggs";

  it("compares line 0 with the name", () => {
    expect(textMatches(hit(0, 0, 8, "Shopping"), name, info)).toBe(true);
    expect(textMatches(hit(0, 0, 4, "milk"), name, info)).toBe(false);
  });

  it("compares other lines with the whole text", () => {
    expect(textMatches(hit(2, 5, 10, "bread"), name, info)).toBe(true);
    expect(textMatches(hit(2, 5, 10, "Shopp"), name, info)).toBe(false);
  });

  it("is false when the text changed after the search", () => {
    expect(textMatches(hit(2, 5, 10, "bread"), name, "milk\nbutter\neggs")).toBe(false);
  });

  it("is false for a range outside the text", () => {
    expect(textMatches(hit(3, 11, 20, "eggs"), name, info)).toBe(false);
    expect(textMatches(hit(1, -1, 3, "mil"), name, info)).toBe(false);
    expect(textMatches(hit(1, 3, 2, ""), name, info)).toBe(false);
  });

  it("jump selects after emoji", () => {
    const text = "😀 needle";
    const h = hit(1, 3, 9, "needle");

    expect(textMatches(h, "n", text)).toBe(true);
    expect(text.slice(h.start, h.end)).toBe("needle");
  });
});

describe("scrollTopFor", () => {
  let area: HTMLTextAreaElement;

  afterEach(() => {
    area.remove();
  });

  it("grows with the number of the line", () => {
    area = document.createElement("textarea");
    area.style.font = "16px monospace";
    area.style.lineHeight = "20px";
    area.style.width = "300px";
    area.style.height = "100px";
    area.value = Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join("\n");
    document.body.append(area);

    const tops = [1, 10, 20, 30, 40].map((line) => scrollTopFor(area, area.value.indexOf(`line ${line}\n`)));

    for (let i = 1; i < tops.length; i++) expect(tops[i]).toBeGreaterThan(tops[i - 1]);
    expect(tops[0]).toBe(0);
  });

  it("leaves no mirror behind", () => {
    area = document.createElement("textarea");
    area.value = "a\nb";
    document.body.append(area);
    const before = document.body.children.length;

    scrollTopFor(area, 2);

    expect(document.body.children.length).toBe(before);
  });
});
