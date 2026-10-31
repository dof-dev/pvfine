import { expect, test, vi } from "vitest";
import { createSSRApp } from "vue";
import { renderToString } from "@vue/server-renderer";
import { createPinia, setActivePinia } from "pinia";
import LogPanel from "../src/components/LogPanel.vue";
import { logCapacity, useLogStore } from "../src/stores/logs";

const virtual = vi.hoisted(() => ({ props: undefined as any }));
vi.mock("naive-ui", async () => {
  const { defineComponent, h } = await import("vue");
  const wrapper = defineComponent({ setup: (_, { slots }) => () => h("div", slots.default?.()) });
  return {
    NButton: wrapper, NCheckbox: wrapper, NCheckboxGroup: wrapper, NIcon: wrapper, NTooltip: wrapper,
    NVirtualList: defineComponent({
      props: {
        items: { type: Array, required: true }, itemSize: Number, itemResizable: Boolean,
        keyField: String, defaultScrollIndex: Number, onScroll: Function,
      },
      setup(props, { slots }) {
        virtual.props = props;
        // 模拟虚拟列表只请求可见行，确认面板没有提前渲染全部条目。
        return () => h("div", props.items.slice(-12).map((item: unknown) => slots.default?.({ item })));
      },
    }),
  };
});

test("满容量日志延迟到展开后渲染，并通过虚拟列表按需显示多行内容", async () => {
  const pinia = createPinia();
  setActivePinia(pinia);
  const logs = useLogStore();
  logs.ingest(Array.from({ length: logCapacity }, (_, index) => ({
    id: index + 1, timestamp: "2026-10-21T00:00:00Z", source: "test", level: "WARN",
    message: `日志 ${index}\n多行内容`,
  })));
  const render = () => renderToString(createSSRApp(LogPanel).use(pinia));
  virtual.props = undefined;
  expect(await render()).not.toContain('class="log-line');
  expect(virtual.props).toBeUndefined();
  logs.expanded = true;
  const html = await render();
  expect(virtual.props.items).toHaveLength(logCapacity);
  expect(virtual.props.itemResizable).toBe(true);
  expect(virtual.props.keyField).toBe("id");
  expect(virtual.props.defaultScrollIndex).toBe(logCapacity - 1);
  expect(html.match(/class="log-line /g)).toHaveLength(12);
  expect(html).toContain(`日志 ${logCapacity - 1}\n多行内容`);

  virtual.props.onScroll({ target: { scrollHeight: 48000, scrollTop: 0, clientHeight: 200 } });
  expect(logs.follow).toBe(false);
  logs.add("ERROR", "test", "最新错误");
  logs.levels = ["ERROR"];
  expect(await render()).toContain("最新错误");
  expect(virtual.props.items).toHaveLength(1);
  expect(virtual.props.defaultScrollIndex).toBeUndefined();
  virtual.props.onScroll({ target: { scrollHeight: 200, scrollTop: 0, clientHeight: 200 } });
  expect(logs.follow).toBe(true);
  logs.clear();
  expect(await render()).toContain("暂无日志");
});
