// Strength of the sync phrase, a hint only (spec §7): 0 (weak) to 4 (strong).
import { ZxcvbnFactory } from "@zxcvbn-ts/core";
import { adjacencyGraphs, dictionary } from "@zxcvbn-ts/language-common";

const zxcvbn = new ZxcvbnFactory({ graphs: adjacencyGraphs, dictionary });

export type Strength = 0 | 1 | 2 | 3 | 4;

export function strength(phrase: string): Strength {
  return phrase === "" ? 0 : zxcvbn.check(phrase).score;
}
