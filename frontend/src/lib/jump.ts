// The jump from a search hit to its place in the editor (search spec §9.2).
import type { SearchHitDTO } from "./api";

/**
 * textMatches tells whether the place of hit still holds its text: the name
 * for line 0, the whole text otherwise. Offsets are UTF-16, as JS strings.
 */
export function textMatches(hit: SearchHitDTO, name: string, info: string): boolean {
  const source = hit.line === 0 ? name : info;
  return 0 <= hit.start && hit.start <= hit.end && hit.end <= source.length && source.slice(hit.start, hit.end) === hit.text;
}

/** MIRRORED are the styles that decide where the text of a textarea wraps. */
const MIRRORED = [
  "boxSizing",
  "fontFamily",
  "fontSize",
  "fontStyle",
  "fontVariant",
  "fontWeight",
  "fontStretch",
  "letterSpacing",
  "wordSpacing",
  "lineHeight",
  "textIndent",
  "textTransform",
  "tabSize",
  "whiteSpace",
  "wordBreak",
  "overflowWrap",
  "paddingTop",
  "paddingLeft",
  "paddingRight",
] as const;

/**
 * scrollTopFor returns the scrollTop of area that brings the visual line with
 * offset to the middle. The line is found in a hidden "mirror" div with the
 * styles and width of area, so wrapped lines count; WebView auto-scrolling
 * after setSelectionRange is not reliable.
 */
export function scrollTopFor(area: HTMLTextAreaElement, offset: number): number {
  const style = getComputedStyle(area);
  const mirror = document.createElement("div");
  for (const key of MIRRORED) mirror.style[key] = style[key];
  mirror.style.position = "absolute";
  mirror.style.visibility = "hidden";
  mirror.style.top = "0";
  mirror.style.left = "-10000px";
  mirror.style.border = "0";
  mirror.style.boxSizing = "content-box";
  mirror.style.whiteSpace = "pre-wrap";
  // clientWidth leaves out the scrollbar, which narrows the text as well.
  const padding = parseFloat(style.paddingLeft) + parseFloat(style.paddingRight);
  mirror.style.width = `${Math.max(area.clientWidth - (padding || 0), 0)}px`;

  const before = area.value.slice(0, offset);
  const marker = document.createElement("span");
  marker.textContent = "​";
  mirror.append(before, marker);
  document.body.append(mirror);
  const line = lineHeight(style);
  // The marker is where the line starts, wrapped lines counted. Without layout
  // (no WebView, as in tests) every element is 0 high: then each line break
  // counts as one line of the style's height.
  const top =
    mirror.offsetHeight > 0
      ? marker.offsetTop
      : (parseFloat(style.paddingTop) || 0) + (before.split("\n").length - 1) * line;
  mirror.remove();

  return Math.max(Math.round(top - Math.max(area.clientHeight - line, 0) / 2), 0);
}

/** lineHeight is the height of one line in px; "normal" is about 1.2 of the font size. */
function lineHeight(style: CSSStyleDeclaration): number {
  const height = parseFloat(style.lineHeight);
  if (!Number.isNaN(height)) return height;
  const size = parseFloat(style.fontSize);
  return (Number.isNaN(size) ? 16 : size) * 1.2;
}
