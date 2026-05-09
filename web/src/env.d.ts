/// <reference types="vite/client" />

// 让 TypeScript 把 .vue 文件视为合法模块——Vite + vue-tsc 链路里
// 仅这一段声明就足够，Vue 3 SFC 内部的类型由 @vue/tsconfig 与 vue-tsc 接管。
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>
  export default component
}
