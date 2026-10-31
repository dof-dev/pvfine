import { beforeEach, expect, it, vi } from "vitest";
import { createPinia, setActivePinia } from "pinia";
import { effectScope, toRef } from "vue";
const events = vi.hoisted(() => new Map<string, () => void>());
vi.mock("@wailsio/runtime", () => ({ Events: { On: (name: string, handler: () => void) => events.set(name, handler) } }));
import { skillTreeCellNode, skillTreeCellSelectionKey } from "../src/gui/skillTree";
import type { SkillTreeNode } from "../bindings/pvfine/services/models";
import { useFileGUIStore } from "../src/stores/fileGUI";

beforeEach(() => { events.clear(); setActivePinia(createPinia()); });
it("弹窗组件销毁后，重新创建的选择引用仍恢复职业、转职和SP/TP", () => {
  const first = effectScope();
  first.run(() => {
    const gui = useFileGUIStore();
    toRef(gui.skillTreeSelection, "job").value = "[swordman]";
    toRef(gui.skillTreeSelection, "branch").value = "weaponmaster";
    toRef(gui.skillTreeSelection, "kind").value = "tp";
  });
  first.stop();
  const reopened = effectScope();
  reopened.run(() => {
    expect(useFileGUIStore().skillTreeSelection).toEqual({job: "[swordman]", branch: "weaponmaster", kind: "tp"});
  });
  reopened.stop();
});

it("各位置独立记忆，切换转职和重开弹窗后恢复，其他技能树不串用", () => {
  const cell = {key: "235:938", nodes: [319, 112].map(id => ({id}) as SkillTreeNode)};
  const key = skillTreeCellSelectionKey("clientonly/skilltree/swordman_sp.co", "[swordman]", "sp", "weaponmaster", cell.key);
  const second = {...cell, key: "329:938"};
  const secondKey = skillTreeCellSelectionKey("clientonly/skilltree/swordman_sp.co", "[swordman]", "sp", "weaponmaster", second.key);
  const first = effectScope();
  first.run(() => {
    const gui = useFileGUIStore();
    gui.skillTreeCellSelections[key] = 319;
    gui.skillTreeCellSelections[secondKey] = 112;
    gui.skillTreeSelection.branch = "asura";
  });
  first.stop();
  const reopened = effectScope();
  reopened.run(() => {
    const gui = useFileGUIStore();
    gui.skillTreeSelection.branch = "weaponmaster";
    // Reloading creates new node objects; restore by ID rather than reference.
    const reloaded = {...cell, nodes: cell.nodes.map(n => ({...n}))};
    expect(skillTreeCellNode(reloaded, gui.skillTreeCellSelections[key]).id).toBe(319);
    expect(skillTreeCellNode(second, gui.skillTreeCellSelections[secondKey]).id).toBe(112);
    for (const context of [
      ["other.co", "[swordman]", "sp", "weaponmaster"],
      ["clientonly/skilltree/swordman_sp.co", "[mage]", "sp", "weaponmaster"],
      ["clientonly/skilltree/swordman_sp.co", "[swordman]", "tp", "weaponmaster"],
      ["clientonly/skilltree/swordman_sp.co", "[swordman]", "sp", "asura"],
    ]) {
      const otherKey = skillTreeCellSelectionKey(context[0]!, context[1]!, context[2]!, context[3]!, cell.key);
      expect(skillTreeCellNode(cell, gui.skillTreeCellSelections[otherKey]).id).toBe(112);
    }
    events.get("archive:opened")!();
    expect(skillTreeCellNode(cell, gui.skillTreeCellSelections[key]).id).toBe(112);
  });
  reopened.stop();
});
