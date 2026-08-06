import { test, describe } from "node:test";
import assert from "node:assert/strict";

import {
  STYLE_SUFFIX,
  appearancesForCharacters,
  buildScenePrompt,
} from "../agent/lib/scene-prompt.ts";
import type { CharacterSheet } from "../agent/lib/campaigns/types.ts";

describe("buildScenePrompt", () => {
  test("joins scene, appearances, setting, theme and style suffix", () => {
    const prompt = buildScenePrompt({
      sceneDescription: "The inn is quiet.",
      appearances: ["Elven rogue with a silver dagger"],
      setting: "Forgotten Realms",
      theme: "mystery",
    });
    assert.equal(
      prompt,
      "The inn is quiet., Elven rogue with a silver dagger, Setting: Forgotten Realms, Theme: mystery, high fantasy, cinematic lighting, detailed illustration",
    );
  });

  test("drops empty setting and theme parts", () => {
    const prompt = buildScenePrompt({
      sceneDescription: "Scene",
      appearances: [],
      setting: "",
      theme: "",
    });
    assert.equal(prompt, `Scene, ${STYLE_SUFFIX}`);
  });

  test("trims whitespace in parts", () => {
    const prompt = buildScenePrompt({
      sceneDescription: "  Scene  ",
      appearances: ["  hero  "],
      setting: "  world  ",
      theme: "",
    });
    assert.ok(prompt.startsWith("Scene, hero,"));
    assert.ok(prompt.includes("Setting:"));
  });
});

describe("appearancesForCharacters", () => {
  const sheets: CharacterSheet[] = [
    {
      id: "1",
      campaignId: "c",
      name: "Aria",
      slug: "aria",
      ownerUserId: "u1",
      characterClass: "rogue",
      race: "elf",
      level: 1,
      stats: {},
      appearance: "Silver-haired elf",
      createdAt: "2026-01-01",
    },
    {
      id: "2",
      campaignId: "c",
      name: "Borin",
      slug: "borin",
      ownerUserId: "u2",
      characterClass: "fighter",
      race: "dwarf",
      level: 2,
      stats: {},
      createdAt: "2026-01-02",
    },
  ];

  test("matches names case-insensitively and returns appearances", () => {
    assert.deepEqual(appearancesForCharacters(["aria", "BORIN"], sheets), [
      "Silver-haired elf",
    ]);
  });

  test("ignores unknown names and sheets without appearance", () => {
    assert.deepEqual(appearancesForCharacters(["Borin", "Nobody", "Aria"], sheets), [
      "Silver-haired elf",
    ]);
  });

  test("handles undefined and empty inputs", () => {
    assert.deepEqual(appearancesForCharacters(undefined, sheets), []);
    assert.deepEqual(appearancesForCharacters([], sheets), []);
    assert.deepEqual(appearancesForCharacters(["  "], sheets), []);
  });
});
