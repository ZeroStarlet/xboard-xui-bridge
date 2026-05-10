/** @type {import('tailwindcss').Config} */
//
// v0.5 视觉重构（ui-ux-pro-max 设计规范）：
//
// 设计目标——现代化、高端、大气、有质感、上档次。具体实现策略：
//
//   a) 色板系统：从 v0.4 单调的 emerald 主色，升级为"主色 brand（青绿）+ 强调
//      accent（靛蓝）+ 中性 surface（slate）"三轨色板。运维场景多在深色机房 /
//      白天双使用，slate 比 gray 冷峻 + 高对比，对屏幕长时间凝视友好。
//
//   b) 阴影层次：tailwind 默认仅提供 sm/md/lg/xl/2xl 五层"统一灰度"阴影；
//      v0.5 引入 soft / glow / float 三组语义阴影——分别用于"卡片浮起感"
//      "焦点高亮"和"模态层悬浮"，让层级语义更清晰。
//
//   c) 圆角阶梯：v0.4 用 rounded-md(6px) / rounded-lg(8px) 偏小；
//      v0.5 改为 lg(12px) / xl(16px) / 2xl(20px) 更圆润现代。
//
//   d) 动画 keyframes：fade-in-up / shimmer / pulse-soft——
//      让卡片进入、按钮 hover、加载态都有微妙动效，对用户感知质感至关重要。
export default {
  content: ['./index.html', './src/**/*.{vue,ts,js}'],
  theme: {
    extend: {
      // 配色——保留 brand 兼容旧 class，同时引入 accent / surface。
      // brand 仍指向 emerald 但调到更"现代感"的 500 而非 v0.4 的 default 0x42b883
      // （后者偏 Vue 官方品牌——v0.5 想让中间件有自己的"工程感"视觉身份，
      // emerald 系列在白底上比 0x42b883 更稳重）。
      colors: {
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
        accent: {
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
        surface: {
          // surface 是页面 + 卡片底色阶梯。50 是页面背景，100 是嵌套区背景，
          // 200 是分割线，900/950 是暗色面板背景。
          50:  '#f8fafc',
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
      // 阴影令牌——用语义命名而非数字阶梯，让组件库使用方一眼看到意图。
      boxShadow: {
        // soft：默认卡片用——精致两层柔和阴影，胜过 tailwind 默认 shadow-sm
        // 单层平阴影。layer-1 给外环柔光，layer-2 给底部锚定感。
        soft: '0 1px 2px 0 rgba(15, 23, 42, 0.04), 0 4px 12px -2px rgba(15, 23, 42, 0.06)',
        // float：模态层 / 弹出 / 抽屉用——更深的阴影 + 略大半径，让浮层"飞起来"。
        float: '0 12px 32px -8px rgba(15, 23, 42, 0.18), 0 4px 12px -4px rgba(15, 23, 42, 0.10)',
        // glow：焦点态用（input focus / 按钮 active）。绿色调让品牌色与交互态强关联。
        glow: '0 0 0 4px rgba(16, 185, 129, 0.15)',
        // glow-accent：accent 色配套 focus 环（用于次要交互）。
        'glow-accent': '0 0 0 4px rgba(59, 130, 246, 0.15)',
      },
      // 圆角阶梯——略向大圆角倾斜，符合 2024+ 现代审美。
      borderRadius: {
        '4xl': '2rem',
      },
      // 字体栈——优先 Inter / SF Pro 等可变字体，兼容旧系统字体。
      // 中文回退到 PingFang/微软雅黑 系列。
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
      // 自定义 keyframes——作 fade-in / shimmer / pulse-soft 三组。
      // 不在 utilities 里加新 class，让消费者用 animate-<name> 就行。
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
      },
      animation: {
        'fade-in-up': 'fade-in-up 0.4s cubic-bezier(0.16, 1, 0.3, 1)',
        'fade-in':    'fade-in 0.3s ease-out',
        shimmer:      'shimmer 2.4s linear infinite',
        'pulse-soft': 'pulse-soft 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
      },
      // backdropBlur 用于玻璃拟态登录页——tailwind 默认已有，这里不扩展。
    },
  },
  plugins: [],
}
