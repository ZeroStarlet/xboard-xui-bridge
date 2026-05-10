// Button 模块 barrel——只 re-export 公共 API，不放任何实现。
//
// 拆分理念（与 badge/alert 同步）：
//   - Button.vue          组件本体
//   - variants.ts         cva 变体（buttonVariants 函数 + ButtonVariants 类型）
//   - index.ts            re-export 上面两条，让消费者一行 import 就能拿到全套
//
// 拆 variants.ts 是为了避免组件 ↔ index.ts 自循环依赖，详见 variants.ts 文件头注释。
export { default as Button } from './Button.vue'
export { buttonVariants, type ButtonVariants } from './variants'
