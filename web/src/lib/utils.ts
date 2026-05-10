// shadcn-vue 全局工具函数库。
//
// cn(...inputs) — 合并任意数量的 class 输入，并按 Tailwind 冲突类组保留后者。
//
//   背景：组件库（shadcn-vue）大量使用 cva（class-variance-authority）生成
//   variant 类列表，再在调用方（业务代码）通过 props 追加 class。两者拼接时
//   会产生形如 "px-4 py-2 ... px-2" 这样的 Tailwind 重复设置——后者本应覆盖
//   前者，但 CSS 实际生效顺序由 stylesheet 中规则的位置决定，并非 class
//   列表中的顺序。tailwind-merge 通过解析 Tailwind 的内置规则表，按"冲突
//   类组 + modifier scope"双键分桶，同桶内只保留 class 列表里最后出现的那个：
//
//     - 同 scope + 同冲突组：如 `px-4 px-2`、`bg-red-500 bg-blue-500`，
//       后者覆盖前者；
//     - 同 scope + 不同冲突组：如 `px-4 py-2`，二者不冲突，并存；
//     - 不同 scope：如 `bg-red-500 hover:bg-blue-500`，hover: 是独立 scope，
//       与默认 scope 不互相冲突，二者并存（hover 时切换为蓝）；
//     - 同 scope + 同组、且都带 modifier：如 `hover:bg-red-500 hover:bg-blue-500`，
//       仍按尾部保留——modifier 不是"豁免冲突"，而是把 scope 切到 hover 子空间；
//     - 自定义类（如 `card` / `btn-primary`，对应 CSS 选择器 .card / .btn-primary）
//       不在 Tailwind 规则表里，原样保留，不参与冲突判定。
//
//   clsx 负责把对象/数组/条件输入扁平化为字符串。
//
//   合并顺序：clsx 先合并 → twMerge 处理冲突类组，调用顺序不可调换
//   （twMerge 只接受字符串输入，不接受 clsx 支持的对象/数组形态）。
//
// 使用示例：
//
//   <Button :class="cn('w-full', loading && 'opacity-60', $attrs.class)" />
//
//   - 业务里通过 v-bind="$attrs" 透传时，外部 class 会出现在最末尾，自然
//     覆盖组件内部默认值，符合"调用方更高优先级"直觉。
//   - 条件类用 boolean && 'class' 写法即可，无需手写三元表达式。
//
// 不依赖 Pinia / vue-router / vue-i18n —— 这是纯类型/字符串工具，不引入
// 任何运行时状态依赖，确保组件库源码可在任何 Vue 项目里复用。
import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/**
 * 合并 class 表达式并消除 Tailwind 冲突类。
 *
 * @param inputs class 列表——支持 string / 对象 / 数组 / 条件表达式
 *               （由 clsx 处理），也支持 undefined / false（自动跳过）。
 * @returns      最终的 class 字符串，可直接绑定到 :class。
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs))
}
