import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import "./style.css";
import { applyTheme, getTheme } from "./theme";

// 主题设置从后端异步加载，先同步应用深色默认值，避免启动时闪白。
applyTheme(getTheme("dark"));

const app = createApp(App);
app.use(createPinia());
app.mount("#app");
