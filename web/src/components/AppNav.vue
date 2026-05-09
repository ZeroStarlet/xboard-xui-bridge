<script setup lang="ts">
// 左侧导航栏。
//
// 路由结构由 router 里的 meta.layout=panel 决定；本组件把"逻辑路由"
// 抽成数组渲染，新增页面只需改 navItems 即可。
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

// 导航项纯文本：emoji 在不同平台/字体下渲染差异大且对屏幕阅读器不友好；
// 当前阶段保持极简文本风格，未来如需图标再统一接入 lucide-vue-next。
const navItems = [
  { to: '/dashboard', label: '仪表盘' },
  { to: '/bridges', label: '桥接管理' },
  { to: '/settings', label: '运行参数' },
  { to: '/account', label: '账户' },
]

async function handleLogout() {
  await auth.logout()
  router.push('/login')
}
</script>

<template>
  <aside class="w-56 shrink-0 bg-brand-dark text-gray-100 flex flex-col" aria-label="主导航">
    <div class="px-6 py-5 border-b border-white/10">
      <h1 class="text-lg font-bold">xboard-xui-bridge</h1>
      <p class="text-xs text-gray-300 mt-1">{{ auth.user?.username || '未登录' }}</p>
    </div>
    <nav class="flex-1 px-3 py-4 space-y-1">
      <RouterLink
        v-for="item in navItems"
        :key="item.to"
        :to="item.to"
        class="block px-3 py-2 rounded text-sm hover:bg-white/10"
        active-class="bg-white/10 font-medium"
      >
        {{ item.label }}
      </RouterLink>
    </nav>
    <div class="px-3 py-4 border-t border-white/10">
      <button class="w-full text-left px-3 py-2 rounded text-sm hover:bg-white/10" @click="handleLogout">
        退出登录
      </button>
    </div>
  </aside>
</template>
