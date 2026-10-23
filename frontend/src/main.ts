import { createApp } from "vue";
import { version } from "../package.json";
import App from "./App.vue";
import "./style.css";

declare const __COMMIT_HASH__: string;

console.info(
  `%c QPix %c v${version}_${__COMMIT_HASH__} `,
  "background:#237a57;color:#fff;padding:3px 0;border-radius:3px 0 0 3px;font-family:monospace",
  "background:#c8f0df;color:#20543f;padding:3px 0;border-radius:0 3px 3px 0;font-family:monospace",
);
console.info(
  `%cCopyright © ${new Date().getFullYear()} Qwerto107. All rights reserved.`,
  "color:#429575;font-family:monospace",
);

createApp(App).mount("#app");
