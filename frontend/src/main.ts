import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import ScriptWindow from "./ScriptWindow.vue";
import "./style.css";
import { installMCPBridge } from "./mcpBridge";
import { applyTheme, getTheme } from "./theme";
import { installLogging } from "./logging";
import { installActiveUndoBridge } from "./activeUndoBridge";

// 安装全局撤销/重做桥接（拦截 Wails 底层 execCommand('undo') 并精确分发给活跃编辑器）
installActiveUndoBridge();

// 主题设置从后端异步加载，先同步应用深色默认值，避免启动时闪白。
applyTheme(getTheme("dark"));

// 资产服务器没有 SPA 回退（未命中的路径直接 404），所以第二个窗口用
// query 而不是路径来选择视图。默认加载主窗口。
const view = new URLSearchParams(window.location.search).get("view");
const root = view === "script" ? ScriptWindow : App;

const app = createApp(root);
app.use(createPinia());
installLogging(app);
app.mount("#app");

installMCPBridge(view === "script");
