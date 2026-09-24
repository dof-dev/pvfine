import { describe, expect, it } from "vitest";
import { getGUIProvider } from "../src/gui/registry";
import { createGUIModes, createWorldDropDraftState, createWorldDropSession } from "../src/gui/state";
import type { WorldDropDocument } from "../bindings/pvfine/services/models";
import {
  batchAddItems,
  batchDeleteItems,
  calcItemPercentage,
  calcLevelTotalWeight,
  countLevelMatches,
  countMatchingItems,
  filterItems,
  findFirstLevelWithMatch,
  getLevelsInInterval,
  isFilterCriteriaActive,
  toEditableLevels,
  toWorldDropLevels,
  validateLevelInterval,
  type WorldDropFilterCriteria,
} from "../src/gui/worldDropForm";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => {
    resolve = yes;
    reject = no;
  });
  return { promise, resolve, reject };
}

describe("world drop GUI registry & session", () => {
  it("首次从工具栏读取时，文档先返回不应被误判为表单修改", async () => {
    const file = { index: 1, path: "etc/worlddrop.etc", text: "[world drop]\n1 0 100 5 -1", editable: true };
    const session = createWorldDropSession(async () => ({
      revision: 1,
      levels: [{ level: 1, items: [{ id: 100, weight: 5, name: "测试道具" }] }],
    }));
    const draft = createWorldDropDraftState();

    expect(draft.isDirty.value).toBe(false);
    await session.load(file, "epoch1");
    // 后端文档已到达，表单尚未同步；这不是用户编辑。
    expect(session.document.value?.levels?.length).toBe(1);
    expect(draft.isDirty.value).toBe(false);
    draft.sync(session.document.value);
    expect(draft.isDirty.value).toBe(false);

    draft.editableLevels.value[0].items[0].weight = 9;
    expect(draft.isDirty.value).toBe(true);
    await session.load(file, "epoch1");
    // 后续读取也不能擅自重置已编辑表单的比较基线。
    expect(draft.isDirty.value).toBe(true);
  });

  it("GUI Provider 注册匹配 etc/worlddrop.etc 及其路径变体，排除其他文件", () => {
    const file = { index: 1, path: "etc/worlddrop.etc", text: "", editable: true };
    const provider = getGUIProvider(file);
    expect(provider).not.toBeNull();
    expect(provider?.id).toBe("world-drop");
    expect(provider?.label).toBe("全局掉率");
    expect(provider?.readOnly).toBe(false);

    // 大小写与斜杠变体
    expect(getGUIProvider({ ...file, path: "ETC/WORLDDROP.ETC" })?.id).toBe("world-drop");
    expect(getGUIProvider({ ...file, path: "etc\\worlddrop.etc" })?.id).toBe("world-drop");
    expect(getGUIProvider({ ...file, path: "/etc/worlddrop.etc" })?.id).toBe("world-drop");

    // 不匹配其他文件
    expect(getGUIProvider({ ...file, path: "etc/itemdropinfo_monster.etc" })).toBeNull();
    expect(getGUIProvider({ ...file, path: "etc/worlddrop.txt" })).toBeNull();
  });

  it("Session 加载与生成代次保护：慢请求被新请求丢弃，不覆盖最新结果", async () => {
    const file = { index: 1, path: "etc/worlddrop.etc", text: "text1", editable: true };
    const firstDeferred = deferred<WorldDropDocument>();
    const secondDeferred = deferred<WorldDropDocument>();

    let callCount = 0;
    const session = createWorldDropSession(async () => {
      callCount++;
      return callCount === 1 ? firstDeferred.promise : secondDeferred.promise;
    });

    const firstLoad = session.load(file, "epoch1");
    expect(session.loading.value).toBe(true);

    // 发起第二次加载
    const secondLoad = session.load(file, "epoch1");

    // 第二次先返回
    secondDeferred.resolve({
      revision: 2,
      levels: [{ level: 85, items: [] }],
    });
    await secondLoad;

    expect(session.document.value?.revision).toBe(2);
    expect(session.selectedLevel.value).toBe(85);

    // 第一次才返回，应当被忽略
    firstDeferred.resolve({
      revision: 1,
      levels: [{ level: 1, items: [] }],
    });
    await firstLoad;

    expect(session.document.value?.revision).toBe(2);
    expect(session.selectedLevel.value).toBe(85);
  });

  it("Session 异常与空数据处理：保留上次成功状态，重试可恢复", async () => {
    const file = { index: 1, path: "etc/worlddrop.etc", text: "", editable: true };
    let shouldFail = false;

    const session = createWorldDropSession(async () => {
      if (shouldFail) throw new Error("解析失败：第 3 行缺少结束标记");
      return {
        revision: 1,
        levels: [{ level: 10, items: [] }],
      };
    });

    await session.load(file, "epoch1");
    expect(session.document.value?.revision).toBe(1);
    expect(session.error.value).toBe("");

    // 下次加载失败
    shouldFail = true;
    await session.load(file, "epoch1");
    expect(session.loading.value).toBe(false);
    expect(session.error.value).toContain("缺少结束标记");
    // 保留上一次成功加载的数据文档
    expect(session.document.value?.revision).toBe(1);

    // 恢复重试
    shouldFail = false;
    await session.load(file, "epoch1");
    expect(session.error.value).toBe("");
    expect(session.document.value?.revision).toBe(1);
  });

  it("Session.load 返回值：成功返回 true，被覆盖或异常返回 false", async () => {
    const file = { index: 1, path: "etc/worlddrop.etc", text: "text1", editable: true };
    const firstDeferred = deferred<WorldDropDocument>();
    const secondDeferred = deferred<WorldDropDocument>();

    let count = 0;
    const session = createWorldDropSession(async () => {
      count++;
      if (count === 1) return firstDeferred.promise;
      if (count === 2) return secondDeferred.promise;
      throw new Error("异常测试");
    });

    const firstLoad = session.load(file, "epoch1");
    const secondLoad = session.load(file, "epoch1");

    secondDeferred.resolve({ revision: 2, levels: [] });
    const secondResult = await secondLoad;
    expect(secondResult).toBe(true);

    firstDeferred.resolve({ revision: 1, levels: [] });
    const firstResult = await firstLoad;
    expect(firstResult).toBe(false);

    // 失败情况
    const thirdResult = await session.load(file, "epoch1");
    expect(thirdResult).toBe(false);
  });

  it("GUI 模式守卫：阻止未保存修改的模式切换；确认放弃修改时重置表单并清除脏状态", async () => {
    const modes = createGUIModes();
    modes.set(1, "gui");
    expect(modes.entries.get(1)?.mode).toBe("gui");

    let isDirty = true;
    let formResetCalled = false;

    // 模拟确认框逻辑：用户确认放弃时重置表单并清除 dirty
    const unregister = modes.registerGuard(1, async () => {
      if (!isDirty) return true;
      // 模拟用户点击【取消】
      return false;
    });

    // 尝试切换到 text -> 被守卫拦截
    const allowedFirst = await modes.requestMode(1, "text");
    expect(allowedFirst).toBe(false);
    expect(modes.entries.get(1)?.mode).toBe("gui");
    expect(formResetCalled).toBe(false);

    // 替换为用户点击【放弃修改】
    unregister();
    modes.registerGuard(1, async () => {
      if (!isDirty) return true;
      // 模拟用户点击【放弃修改并离开】
      formResetCalled = true;
      isDirty = false;
      return true;
    });

    const allowedSecond = await modes.requestMode(1, "text");
    expect(allowedSecond).toBe(true);
    expect(modes.entries.get(1)?.mode).toBe("text");
    expect(formResetCalled).toBe(true);
    expect(isDirty).toBe(false);
  });

  it("不同窗格的 GUI 模式与守卫独立隔离", async () => {
    const pane1Modes = createGUIModes();
    const pane2Modes = createGUIModes();

    pane1Modes.set(1, "gui");
    pane2Modes.set(1, "text");

    expect(pane1Modes.entries.get(1)?.mode).toBe("gui");
    expect(pane2Modes.entries.get(1)?.mode).toBe("text");

    // 在 pane1 上注册拦截守卫
    pane1Modes.registerGuard(1, () => false);

    // pane1 无法切回 text
    expect(await pane1Modes.requestMode(1, "text")).toBe(false);
    expect(pane1Modes.entries.get(1)?.mode).toBe("gui");

    // pane2 没有守卫，可自由切换
    expect(await pane2Modes.requestMode(1, "gui")).toBe(true);
    expect(pane2Modes.entries.get(1)?.mode).toBe("gui");
    expect(await pane2Modes.requestMode(1, "text")).toBe(true);
    expect(pane2Modes.entries.get(1)?.mode).toBe("text");
  });

  it("标签切换与外部文本变动状态转移逻辑：切出再切回保留脏表单，文本冲突安全标示", () => {
    // 模拟 WorldDropViewer 中的核心 watch 状态机行为
    let loadedText = "version 1 text";
    let loadedArchiveId = "1\u0000etc/worlddrop.etc\u0000epoch1";
    let formDirty = true;
    let staleConflict = false;
    let reloadCount = 0;

    function handleFileChange(active: boolean, index: number, path: string, text: string, epoch: string) {
      if (!active) return;
      const archiveId = `${index}\u0000${path}\u0000${epoch}`;
      const identityChanged = loadedArchiveId !== "" && loadedArchiveId !== archiveId;
      const textChanged = loadedText !== "" && loadedText !== text;

      if (identityChanged) {
        reloadCount++;
        loadedArchiveId = archiveId;
        loadedText = text;
        formDirty = false;
        staleConflict = false;
        return;
      }

      if (textChanged) {
        if (formDirty) {
          staleConflict = true;
        } else {
          reloadCount++;
          loadedText = text;
        }
      }
    }

    // 1. 用户编辑后成为 dirty，随后切到其他标签（active = false）
    handleFileChange(false, 1, "etc/worlddrop.etc", "version 1 text", "epoch1");
    expect(formDirty).toBe(true);
    expect(reloadCount).toBe(0);

    // 2. 切回该标签（active = true），文本与归档未变 -> 必须保留 dirty，绝不 reload
    handleFileChange(true, 1, "etc/worlddrop.etc", "version 1 text", "epoch1");
    expect(formDirty).toBe(true);
    expect(reloadCount).toBe(0);
    expect(staleConflict).toBe(false);

    // 3. 此时外部在分屏中修改了文本（DSL 变动）
    handleFileChange(true, 1, "etc/worlddrop.etc", "version 2 external text", "epoch1");
    // 因存在未应用修改，必须标示 conflict 且保留 dirty 表单，禁止隐式 reload 冲掉用户编辑
    expect(formDirty).toBe(true);
    expect(reloadCount).toBe(0);
    expect(staleConflict).toBe(true);

    // 4. 用户手动点击重新加载并确认放弃后
    reloadCount++;
    loadedText = "version 2 external text";
    formDirty = false;
    staleConflict = false;
    expect(formDirty).toBe(false);
    expect(staleConflict).toBe(false);

    // 5. 在非 dirty 状态下，外部文本更新会自动重新加载
    handleFileChange(true, 1, "etc/worlddrop.etc", "version 3 text", "epoch1");
    expect(reloadCount).toBe(2);
    expect(staleConflict).toBe(false);
  });

  it("Reload 异步竞态防御：读取期间文本发生漂移时拒绝覆盖并标示冲突，且不丢弃脏表单", async () => {
    // 模拟 WorldDropViewer.reload 内部防御机制
    const readDeferred = deferred<WorldDropDocument>();
    const file = { index: 1, path: "etc/worlddrop.etc", text: "text v1", editable: true };
    const guiEpoch = 1;

    let loadedText = "";
    let loadedArchiveIdentity = "";
    let staleConflict = false;
    let syncCount = 0;
    let isDirty = false;

    async function reload(force = false): Promise<void> {
      const userConfirmedDiscard = force;
      if (isDirty && !userConfirmedDiscard) {
        return;
      }

      // 请求发起时快照
      const requestText = file.text;
      const requestArchiveIdentity = `${file.index}\u0000${file.path}\u0000${guiEpoch}`;

      await readDeferred.promise;

      // 检查变动
      const currentArchiveIdentity = `${file.index}\u0000${file.path}\u0000${guiEpoch}`;
      const textChangedDuringLoad = file.text !== requestText;
      const identityChangedDuringLoad = currentArchiveIdentity !== requestArchiveIdentity;

      if (textChangedDuringLoad || identityChangedDuringLoad) {
        staleConflict = true;
        return;
      }

      if (isDirty && !userConfirmedDiscard) {
        staleConflict = true;
        return;
      }

      loadedText = requestText;
      loadedArchiveIdentity = requestArchiveIdentity;
      syncCount++;
      staleConflict = false;
    }

    // 场景 A: 异步读取期间，外部 DSL 文本被修改为 text v2
    const reloadPromiseA = reload(false);
    // 模拟读取中途，用户在分屏编辑 DSL 改变了 file.text
    file.text = "text v2 in split pane";

    // 异步读取完成
    readDeferred.resolve({ revision: 1, levels: [] });
    await reloadPromiseA;

    // 验证：检测到竞态漂移，绝不同步旧文档，绝不将 v2 标为已加载基线，标示冲突
    expect(staleConflict).toBe(true);
    expect(syncCount).toBe(0);
    expect(loadedText).toBe(""); // 没有被错误更新为 text v2

    // 场景 B: 异步读取期间，用户在界面中进行了编辑产生了 dirty 表单
    const secondDeferred = deferred<WorldDropDocument>();
    file.text = "text v2";
    staleConflict = false;
    isDirty = false;

    async function secondReload(): Promise<void> {
      const userConfirmedDiscard = false;
      const requestText = file.text;
      const requestArchiveIdentity = `${file.index}\u0000${file.path}\u0000${guiEpoch}`;

      await secondDeferred.promise;

      const currentArchiveIdentity = `${file.index}\u0000${file.path}\u0000${guiEpoch}`;
      if (file.text !== requestText || currentArchiveIdentity !== requestArchiveIdentity) {
        staleConflict = true;
        return;
      }

      if (isDirty && !userConfirmedDiscard) {
        // 保护脏表单不被静默覆盖
        staleConflict = true;
        return;
      }

      loadedText = requestText;
      syncCount++;
    }

    const reloadPromiseB = secondReload();
    // 异步在途期间，用户点击了添加道具/修改等级，表单变为 dirty
    isDirty = true;

    secondDeferred.resolve({ revision: 2, levels: [] });
    await reloadPromiseB;

    // 验证：保护 dirty 表单，未静默覆盖，且标示冲突
    expect(isDirty).toBe(true);
    expect(syncCount).toBe(0);
    expect(staleConflict).toBe(true);
  });

  describe("全局掉率 GUI 后续特性集成测试 (等级只读、区间添加、批量删除、道具筛选)", () => {
    it("区间选择必须限制在已有配置等级中，且仅对区间内已有等级生效", () => {
      const docLevels = [
        { level: 1, items: [] },
        { level: 15, items: [] },
        { level: 85, items: [] },
      ];
      const editable = toEditableLevels(docLevels);
      const existingLevels = editable.map((l) => l.level);

      // 非法等级值校验
      expect(validateLevelInterval(5, 20, existingLevels)).toContain("Lv. 5 不存在");
      expect(validateLevelInterval(15, 99, existingLevels)).toContain("Lv. 99 不存在");
      expect(validateLevelInterval(85, 15, existingLevels)).toContain("不能大于");

      // 合法区间
      expect(validateLevelInterval(1, 85, existingLevels)).toBe("");

      // 区间内已有等级
      const targets = getLevelsInInterval(editable, 1, 15);
      expect(targets.map((t) => t.level)).toEqual([1, 15]);
    });

    it("添加道具时区间批量追加：已存在同 ID 道具时直接追加新行", () => {
      const docLevels = [
        { level: 1, items: [{ id: 1001, weight: 50, name: "原物品" }] },
        { level: 10, items: [] },
      ];
      const editable = toEditableLevels(docLevels);

      // 批量添加 1001，权重 80
      const res = batchAddItems(editable, 1, 10, {
        id: 1001,
        weight: 80,
        name: "新添加物品",
      });

      expect(res.addedCount).toBe(2);
      expect(res.affectedLevels).toEqual([1, 10]);

      // 等级 1 现在有 2 条记录，第一条为旧物品，第二条为追加行
      expect(editable[0].items.length).toBe(2);
      expect(editable[0].items[0]).toMatchObject({ id: 1001, weight: 50 });
      expect(editable[0].items[1]).toMatchObject({ id: 1001, weight: 80 });

      // 等级 10 现在有 1 条记录
      expect(editable[1].items.length).toBe(1);
      expect(editable[1].items[0]).toMatchObject({ id: 1001, weight: 80 });
    });

    it("批量删除：选定区间内完全移除匹配道具，保留空等级，0 匹配无变更", () => {
      const docLevels = [
        {
          level: 1,
          items: [
            { id: 2001, weight: 10, name: "X" },
            { id: 2001, weight: 20, name: "X2" },
            { id: 3001, weight: 30, name: "Y" },
          ],
        },
        { level: 20, items: [{ id: 2001, weight: 40, name: "X" }] },
        { level: 85, items: [{ id: 2001, weight: 90, name: "X-outside" }] },
      ];
      const editable = toEditableLevels(docLevels);

      // 先查询 [1, 20] 内的匹配情况
      const preview = countMatchingItems(editable, 1, 20, 2001);
      expect(preview.matchingLevels).toEqual([1, 20]);
      expect(preview.matchingRows).toBe(3);

      // 执行批量删除
      const result = batchDeleteItems(editable, 1, 20, 2001);
      expect(result.removedCount).toBe(3);
      expect(result.affectedLevels).toEqual([1, 20]);

      // 等级 1 仅剩下 3001
      expect(editable[0].items.map((i) => i.id)).toEqual([3001]);
      // 等级 20 变为空等级（空等级结构保留，未被删除）
      expect(editable[1].items.length).toBe(0);
      expect(editable[1].level).toBe(20);
      // 等级 85 在区间外，依然保留 2001
      expect(editable[2].items.map((i) => i.id)).toEqual([2001]);

      // 再次对不存在的道具批量删除：0 匹配，无变动
      const zeroResult = batchDeleteItems(editable, 1, 20, 9999);
      expect(zeroResult.removedCount).toBe(0);
      expect(zeroResult.affectedLevels).toEqual([]);
    });

    it("视图筛选：仅展示匹配道具，但总权重与参考百分比始终基于等级内全部道具真实计算", () => {
      const level = {
        level: 1,
        key: 1,
        items: [
          { id: 10, weight: 100, name: "红药水", key: 1 },
          { id: 20, weight: 300, name: "蓝药水", key: 2 },
          { id: 30, weight: 600, name: "圣水", key: 3 },
        ],
      };

      // 全量总权重为 1000
      const totalWeight = calcLevelTotalWeight(level.items);
      expect(totalWeight).toBe(1000);

      // 用户筛选 "红药水"
      const filtered = filterItems(level.items, "红药水");
      expect(filtered.length).toBe(1);
      expect(filtered[0].id).toBe(10);

      // 关键正确性：虽然视图只展示了红药水一条，但概率计算必须使用等级真实总权重 1000（占比 10.00%），而不是 100/100 = 100.00%
      const percentage = calcItemPercentage(filtered[0].weight, totalWeight);
      expect(percentage).toBe("10.00%");

      // 若总权重为 0，展示 0.00% 而不是 NaN
      const zeroLevelTotal = 0;
      expect(calcItemPercentage(filtered[0].weight, zeroLevelTotal)).toBe("0.00%");
    });

    it("提交应用时即使界面处于筛选状态，快照仍完整保留所有未展示的道具行", () => {
      const editable = toEditableLevels([
        {
          level: 10,
          items: [
            { id: 101, weight: 50, name: "武器A" },
            { id: 102, weight: 60, name: "防具B" },
          ],
        },
      ]);

      // 界面处于筛选状态，只看到 "武器A"
      const displayed = filterItems(editable[0].items, "武器");
      expect(displayed.length).toBe(1);

      // 导出提交快照（toWorldDropLevels）
      const snapshot = toWorldDropLevels(editable);
      expect(snapshot[0].items.length).toBe(2);
      expect(snapshot[0].items.map((i) => i.id)).toEqual([101, 102]);
    });

    it("从物品库精准选取物品时：仅按 item.id 全等匹配，排除同名异 ID 和数字子串冲突", () => {
      const docLevels = [
        {
          level: 1,
          items: [
            { id: 10, weight: 100, name: "强化券" },
            { id: 100, weight: 200, name: "高级强化券" }, // 数字包含 10
            { id: 210, weight: 300, name: "特级强化券" }, // 数字包含 10
            { id: 999, weight: 400, name: "强化券" }, // 同名为 "强化券"，但 ID 为 999
          ],
        },
      ];
      const editable = toEditableLevels(docLevels);

      // 1. 精准选取 hit.id = 10（无论名称为何）
      const exactFilter: WorldDropFilterCriteria = { exactItemId: 10 };
      const matched = filterItems(editable[0].items, exactFilter);

      // 必须且仅有一项：id 严格为 10
      expect(matched.length).toBe(1);
      expect(matched[0].id).toBe(10);
      expect(matched[0].name).toBe("强化券");

      // 确认同名异 ID (999) 和包含 10 的 (100, 210) 绝不出现
      expect(matched.some((i) => i.id === 999)).toBe(false);
      expect(matched.some((i) => i.id === 100)).toBe(false);
      expect(matched.some((i) => i.id === 210)).toBe(false);

      // 2. 确认全量提交快照依然不受精准筛选影响，保持全量行完整
      const snapshot = toWorldDropLevels(editable);
      expect(snapshot[0].items.length).toBe(4);
      expect(snapshot[0].items.map((i) => i.id)).toEqual([10, 100, 210, 999]);
    });

    it("精准筛选与自由文本筛选独立建模且支持清除", () => {
      const items = [
        { id: 50, weight: 10, name: "A", key: 1 },
        { id: 500, weight: 20, name: "A-500", key: 2 },
      ];

      // 精准筛选状态
      const exact: WorldDropFilterCriteria = { exactItemId: 50 };
      expect(isFilterCriteriaActive(exact)).toBe(true);
      expect(filterItems(items, exact).map((i) => i.id)).toEqual([50]);

      // 清除精准筛选
      const cleared: WorldDropFilterCriteria = { exactItemId: null, query: "" };
      expect(isFilterCriteriaActive(cleared)).toBe(false);
      expect(filterItems(items, cleared).map((i) => i.id)).toEqual([50, 500]);

      // 自由文本筛选状态
      const textSearch: WorldDropFilterCriteria = { query: "50" };
      expect(isFilterCriteriaActive(textSearch)).toBe(true);
      expect(filterItems(items, textSearch).map((i) => i.id)).toEqual([50, 500]);
    });

    it("精准选择物品时，若当前等级无匹配，导航优先跳转到首个有匹配的等级", () => {
      const docLevels = [
        { level: 1, items: [{ id: 10, weight: 50, name: "药水" }] },
        { level: 10, items: [] }, // 空等级
        { level: 20, items: [{ id: 8888, weight: 100, name: "史诗装备" }] },
        { level: 85, items: [{ id: 8888, weight: 200, name: "史诗装备" }] },
      ];
      const editable = toEditableLevels(docLevels);

      // 模拟当前选中 Level 1
      let selectedLevel: number = 1;
      const targetExactId = 8888;
      const criteria: WorldDropFilterCriteria = { exactItemId: targetExactId };

      // 当前 Level 1 匹配数为 0
      const currentLevelMatches = countLevelMatches(editable[0], criteria);
      expect(currentLevelMatches).toBe(0);

      // 执行导航偏好判定：找到首个有匹配的等级 (应该为 Level 20，而不是 Level 10 或 Level 85)
      if (currentLevelMatches === 0) {
        const firstMatchLevel = findFirstLevelWithMatch(editable, criteria);
        if (firstMatchLevel !== null) {
          selectedLevel = firstMatchLevel;
        }
      }

      expect(selectedLevel).toBe(20);

      // 若再次选择 8888，因当前等级 Level 20 已有匹配，保持当前 Level 20
      const newLevelMatches = countLevelMatches(editable[2], criteria);
      expect(newLevelMatches).toBe(1);
      // 无需跳转
      expect(selectedLevel).toBe(20);
    });
  });
});

