/** @type {import('tailwindcss').Config} */
//
// v0.6 视觉重构（shadcn-vue + Fluent Design + dark mode）：
//
// 设计目标——基于 shadcn-vue 组件体系，对齐微软 Fluent Design 美学，并支持
// 跟随系统的深色模式。具体实现策略：
//
//   a) 双轨色系：
//      - 语义 token（shadcn-vue 约定）—— 用 HSL CSS variables 注入，深浅模式
//        切换只换 :root 与 .dark 下的变量值，组件源码无需感知。
//      - 项目专用色板（brand/info/surface）—— 保留全色阶供 .pill / .alert /
//        .data-table 等"基于色阶层级"的旧 utility class 引用。两轨并行：
//        组件库（components/ui/*）只用语义 token，业务页面老 class 用色阶。
//
//   b) 历史命名迁移：
//      - v0.5 的 `accent`（蓝色调色板）改名为 `info`，把 `accent` 的位置让给
//        shadcn-vue 的语义 token（bg-accent / text-accent-foreground）。
//        视图文件里 `accent-50/100/...` 已 grep 替换为 `info-*`，
//        boxShadow 里 `glow-accent` 也改名 `glow-info`。这次重命名让蓝色调
//        与"shadcn 语义 accent"两个含义彻底分离，避免维护者混淆。
//
//   c) 圆角阶梯下调（Fluent 工程感）：
//      - --radius=0.5rem (8px) 作为单一来源，sm/md/lg 通过 calc 派生。
//      - v0.5 默认 lg=12px / xl=16px / 2xl=20px 偏圆，与 Material 调性更近；
//        v0.6 走 Fluent 路线，整体下调 1 档（按钮/卡片/输入框统一 8px），
//        视觉更"工程"、更"硬"，符合运维后台调性。
//      - rounded-xl/2xl 仍保留（Tailwind 默认提供），仅 lg/md/sm 三档跟随
//        --radius 变量，便于"全站圆角整体微调"——只改一个 CSS variable
//        就能让所有 shadcn 组件同步变形。
//
//   d) 深色模式（class 策略）：
//      - darkMode: 'class' —— 给 <html> 加 .dark 即切换深色，由 stores/theme.ts
//        在 batch 3 引入；prefers-color-scheme 的"system"态由 store 监听 +
//        toggle class 实现，不依赖 'media' 自动切（让用户在 web 面板内可手动
//        覆盖系统设置）。
//      - 所有语义 token 在 :root（light）与 .dark 都给完整定义，shadcn 组件
//        切深色无需改代码。
//      - body 全局 bg-background text-foreground，深色下背景为接近 surface-950
//        的极深蓝灰，机房屏幕长时凝视更友好。
//
//   e) Fluent 微动画（保留 v0.5 + 新增 accordion）：
//      - fade-in-up / fade-in / shimmer / pulse-soft（v0.5 沿用，运维感熟悉）；
//      - accordion-down / accordion-up（shadcn-vue Accordion / Collapsible 用），
//        由 tailwindcss-animate 提供基础 keyframe，本配置注册并暴露 animate-*
//        utility，与项目其他动画一致命名。
//
//   f) tailwindcss-animate 插件：
//      - shadcn-vue 大量组件（Dialog/Sheet/Dropdown/Tooltip/Popover）出场动画
//        依赖 data-[state=open]:animate-in data-[state=closed]:animate-out
//        以及 data-[side=*]:slide-in-from-* 等 utility，全由 tailwindcss-animate
//        生成；不装这个插件，组件能渲染但弹出/关闭无过渡，体验降级显著。
//      - 该插件是构建期 plugin（PostCSS 链路），运行时 0 字节增量。
import animate from 'tailwindcss-animate'

export default {
  // class 策略：通过给 <html> 元素添加/移除 .dark class 切换深色模式。
  // 不用 'media' 是因为：用户可能希望面板内固定亮/深，独立于系统主题；
  // stores/theme.ts 会管理三态（light / dark / system），system 态时再监听
  // matchMedia('prefers-color-scheme: dark') 同步切换。
  darkMode: 'class',
  content: ['./index.html', './src/**/*.{vue,ts,js}'],
  theme: {
    // container 默认配置：让 shadcn-vue 模板中的 .container 类有可预期行为。
    // 项目业务页用的是 mx-auto max-w-7xl，不依赖 container；保留是为了让
    // 后续直接 copy 自 shadcn-vue 文档的范例代码也能正常渲染。
    container: {
      center: true,
      padding: '2rem',
      screens: { '2xl': '1400px' },
    },
    extend: {
      // ============================================================
      // 配色——双轨：语义 token（HSL var）+ 项目专用色板（直接十六进制）
      // ============================================================
      colors: {
        // 语义 token（shadcn-vue 约定）。所有值都从 :root / .dark 下的 CSS
        // variable 解析，确保深浅模式切换只需改一组变量。
        //
        // 命名约定：
        //   - 单一名（如 background）：表示该语义的"主色"，可直接 bg-background。
        //   - {DEFAULT, foreground}：DEFAULT 是背景色，foreground 是其上文字色。
        //     例如 bg-primary text-primary-foreground 永远保证可读对比度。
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
        popover: {
          DEFAULT: 'hsl(var(--popover))',
          foreground: 'hsl(var(--popover-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))',
        },

        // ============================================================
        // 项目专用色板——保留 v0.5 既有色阶供"色阶层级"型 utility 引用。
        // 这些不通过 CSS variable，固定为同一组色阶；深色模式下也用同色阶
        // （shadcn 组件用语义 token 自动深色，下面这些色阶仅供旧 utility
        // class 引用，不影响新组件深色行为）。
        // ============================================================

        // brand：emerald 色阶。.btn-primary 渐变 / .pill-success / .text-gradient-brand
        // 等均引用此色阶。
        //
        // 与语义 token 的关系：v0.6 起 --primary 选用 brand-700（emerald-700 ≈
        // #047857），而非 brand-500——目的是让 bg-primary + 白文字达到 WCAG AA
        // 正文 4.5:1 阈值（详见 style.css 中 :root --primary 的注释）。所以
        // bg-brand-700 与 bg-primary 视觉同源，bg-brand-500 仅作"鲜亮品牌色"
        // 用在 .pill-success 这类小色块徽标场景（小色块 + 大对比度边框 + 短文本，
        // 视觉冲击力比 AA 严苛指标更重要）。
        brand: {
          50: '#ecfdf5',
          100: '#d1fae5',
          200: '#a7f3d0',
          300: '#6ee7b7',
          400: '#34d399',
          500: '#10b981',
          600: '#059669',
          700: '#047857',
          800: '#065f46',
          900: '#064e3b',
          DEFAULT: '#10b981',
          dark: '#065f46',
        },
        // info：原 v0.5 的 accent（蓝色调）改名而来。pill-info / .alert-info
        // 等"信息"语义类引用此色阶；与 shadcn 语义 accent（中性强调）解耦。
        info: {
          50: '#eff6ff',
          100: '#dbeafe',
          200: '#bfdbfe',
          300: '#93c5fd',
          400: '#60a5fa',
          500: '#3b82f6',
          600: '#2563eb',
          700: '#1d4ed8',
          800: '#1e40af',
          900: '#1e3a8a',
          DEFAULT: '#3b82f6',
        },
        // surface：slate 灰阶。页面背景 / 卡片嵌套区 / 分割线等"中性层级"
        // 表达。深色模式下页面真正背景由 --background 接管，但 .card-flat /
        // .input / .data-table 等仍引用 surface 色阶——它们在深色下应表现为
        // 透明叠加（让 .dark 下视觉差异由 token 完成），所以保留色阶不变。
        surface: {
          50: '#f8fafc',
          100: '#f1f5f9',
          200: '#e2e8f0',
          300: '#cbd5e1',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#0f172a',
          950: '#020617',
        },
      },

      // ============================================================
      // 圆角阶梯——以 --radius 为单一来源，下调到 Fluent 风的工程感。
      // ============================================================
      borderRadius: {
        // sm/md/lg 全部派生自 --radius (默认 0.5rem = 8px)：
        //   lg = 8px (Fluent 卡片/Dialog 标准)
        //   md = 6px (按钮/输入)
        //   sm = 4px (badge/tag)
        // 这样切换全站圆角风格只需改一处 :root --radius 值。
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
        // xl/2xl/3xl/4xl 保留 v0.5 既有值——它们用于卡片堆叠、登录页玻璃拟态等
        // 强调"圆润、高端"的场景，不参与 --radius 派生。
        '4xl': '2rem',
      },

      // ============================================================
      // 阴影令牌（v0.5 沿用 + 重命名 glow-accent → glow-info 与色阶迁移同步）
      // ============================================================
      boxShadow: {
        // soft：默认卡片用——精致两层柔和阴影。
        soft: '0 1px 2px 0 rgba(15, 23, 42, 0.04), 0 4px 12px -2px rgba(15, 23, 42, 0.06)',
        // float：模态层 / 弹出 / 抽屉用。
        float: '0 12px 32px -8px rgba(15, 23, 42, 0.18), 0 4px 12px -4px rgba(15, 23, 42, 0.10)',
        // glow：input focus / 按钮 active 用，绿色调与品牌色强关联。
        glow: '0 0 0 4px rgba(16, 185, 129, 0.15)',
        // glow-info：原 glow-accent 改名（与 accent → info 迁移同步），
        // 用于次要交互的 focus 环。
        'glow-info': '0 0 0 4px rgba(59, 130, 246, 0.15)',
      },

      // ============================================================
      // 字体栈——v0.5 沿用，Inter 在 Fluent / Material 体系都兼容。
      // ============================================================
      fontFamily: {
        sans: [
          'Inter',
          'SF Pro Display',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'PingFang SC',
          'Microsoft YaHei',
          'Roboto',
          'Helvetica Neue',
          'sans-serif',
        ],
        mono: [
          'JetBrains Mono',
          'SF Mono',
          'Menlo',
          'Consolas',
          'Liberation Mono',
          'monospace',
        ],
      },

      // ============================================================
      // keyframes 与 animation——v0.5 沿用 + 新增 accordion（shadcn-vue
      // Accordion 组件展开 / 收起所需）。命名约定：
      //
      //   accordion-down：高度从 0 过渡到 var(--reka-accordion-content-height)；
      //   accordion-up：  反向。
      //
      //   --reka-accordion-content-height 由 reka-ui 的 Accordion 组件在运行时
      //   测量内容元素 scrollHeight 后注入到 [data-state] 元素的 inline style 上，
      //   本配置无需感知具体数值。reka-ui 是 shadcn-vue 当前 latest 轨道使用的
      //   primitive 库（替代旧 radix-vue 轨道，变量名也从 --radix-* 更名为
      //   --reka-*），如果以后切回 radix-vue 需把变量名改回 --radix-*。
      //
      //   注意：Collapsible 是不同的 reka-ui primitive，暴露
      //   --reka-collapsible-content-height（不是 accordion 的）；本批次
      //   仅添加 accordion 动画，将来引入 Collapsible 组件时再加对应 keyframes。
      // ============================================================
      keyframes: {
        'fade-in-up': {
          '0%':   { opacity: '0', transform: 'translateY(8px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' },
        },
        'fade-in': {
          '0%':   { opacity: '0' },
          '100%': { opacity: '1' },
        },
        shimmer: {
          '0%':   { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' },
        },
        'pulse-soft': {
          '0%, 100%': { opacity: '1' },
          '50%':      { opacity: '0.65' },
        },
        'accordion-down': {
          from: { height: '0' },
          to:   { height: 'var(--reka-accordion-content-height)' },
        },
        'accordion-up': {
          from: { height: 'var(--reka-accordion-content-height)' },
          to:   { height: '0' },
        },
      },
      animation: {
        'fade-in-up':     'fade-in-up 0.4s cubic-bezier(0.16, 1, 0.3, 1)',
        'fade-in':        'fade-in 0.3s ease-out',
        shimmer:          'shimmer 2.4s linear infinite',
        'pulse-soft':     'pulse-soft 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        'accordion-down': 'accordion-down 0.2s ease-out',
        'accordion-up':   'accordion-up 0.2s ease-out',
      },
    },
  },
  plugins: [
    // tailwindcss-animate：为 shadcn-vue 组件提供 data-[state=*]:animate-in /
    // animate-out / slide-in-from-* / fade-in-* 等 utility。详见上方注释。
    animate,
  ],
}
