// Button cva 变体定义（独立模块，避免组件本体 ↔ barrel index.ts 自循环依赖）。
//
// 拆出原因：
//
//   v0.6 初版把 buttonVariants 写在 button/index.ts 里，Button.vue 从 '.'
//   import variants，index.ts 又用 `export { default as Button } from './Button.vue'`
//   re-export 组件本体——形成"组件 ⇄ barrel"双向引用。
//
//   后果：HMR 顺序敏感、tree-shaking 分析增加难度、生产构建一切正常但
//   开发期偶发"组件未定义"或类型推断异常。把 variants 拆到独立文件让
//   依赖图变成线性 DAG（Button 依赖 variants，index.ts 依赖 Button + variants），
//   两侧 import 都不再触及对方。
//
// 维护惯例：每个 cva-based 组件家族都遵循同一拆分（badge/variants.ts、
// alert/variants.ts 同理），方便维护者一眼识别"哪个文件存样式约定"。
import { cva, type VariantProps } from 'class-variance-authority'

/**
 * cva 实例：buttonVariants(props) → string class 列表。
 *
 * 第一参数是基础类（所有变体共享）；第二参数 variants 子树定义 variant/size
 * 两个维度的变体类；defaultVariants 是消费者不传任何变体值时的回退。
 *
 * 关键设计点：
 *   - focus-visible:ring + ring-offset-background：深色模式下 ring 与按钮间
 *     的间隙色跟随 background，不再露出突兀白边（与 style.css .btn 同理）
 *   - disabled:pointer-events-none + opacity-50：键鼠都不响应，视觉变灰
 *   - has-[>svg]:px-* utility：当 button 内首层有 svg 子节点时自动收紧水平
 *     padding，让"图标 + 文字"组合的视觉重心居中（图标已自带视觉占位）
 *   - [&_svg]:size-4 [&_svg]:shrink-0：所有 button 内的 svg 默认 16px 不变形
 */
export const buttonVariants = cva(
  // 基础类——所有变体共享的"按钮基本盘"。
  [
    'inline-flex items-center justify-center gap-2',
    'whitespace-nowrap rounded-md text-sm font-medium',
    'transition-all duration-150 ease-out',
    'disabled:pointer-events-none disabled:opacity-50',
    'focus-visible:outline-none focus-visible:ring-2',
    'focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
    '[&_svg]:size-4 [&_svg]:shrink-0',
  ].join(' '),
  {
    variants: {
      variant: {
        default:     'bg-primary text-primary-foreground shadow-sm hover:bg-primary/90 active:bg-primary/95',
        destructive: 'bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90 active:bg-destructive/95',
        outline:     'border bg-background text-foreground shadow-sm hover:bg-accent hover:text-accent-foreground',
        secondary:   'bg-secondary text-secondary-foreground shadow-sm hover:bg-secondary/80',
        ghost:       'text-foreground hover:bg-accent hover:text-accent-foreground',
        link:        'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2 has-[>svg]:px-3',
        sm:      'h-8 rounded-md px-3 text-[13px] has-[>svg]:px-2.5',
        lg:      'h-10 rounded-md px-6 has-[>svg]:px-4',
        icon:    'size-9',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

/**
 * Button 变体 prop 的字面值类型——例如 ButtonVariants['variant'] 是
 * 'default' | 'destructive' | 'outline' | 'secondary' | 'ghost' | 'link'。
 * Button.vue 用此类型给 props 加严格类型注解。
 */
export type ButtonVariants = VariantProps<typeof buttonVariants>
