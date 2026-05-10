<script setup lang="ts">
// 桥接管理（v0.7 视觉重构 — Bento Live Console）。
//
// v0.7 与 v0.6 差异：
//
//   1. 列表样式：从 Table 改为响应式卡片网格——与 Dashboard 桥接卡片
//      视觉同源（同一种 protocol-chip + LiveDot + IDs 布局），让"看
//      列表" 与"看仪表盘"无认知断点。
//
//   2. 卡片操作：编辑 / 删除按钮在 group-hover 时浮现——平时视觉干净，
//      鼠标悬停才有交互暗示，符合"工程感运维中心"调性。
//
//   3. 抽屉表单 / 删除确认 Dialog：保留 v0.6 实现（Sheet + Dialog 已
//      包含完整 focus trap + ESC 关闭 + 焦点归还）。Sheet content 内部
//      微调间距让 Bento 风更紧凑。
//
// 数据流：
//
//   - onMounted refresh()，submit / confirmDelete 后再次 refresh()。
//     桥接列表不走 useStatus 共享 composable——它是 / 端点不同
//     （/api/bridges vs /api/status），且仅本视图需要列表本身。
//
// i18n：所有 bridges.* / common.* 文案走 t()，与 v0.6 一致。
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Pencil, Trash2, Loader2, AlertCircle } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from '@/components/ui/sheet'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog'
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import LiveDot from '@/components/LiveDot.vue'
import { useToast } from '@/composables/useToast'
import { api, type Bridge } from '@/api/client'

const { t } = useI18n()
const { toast } = useToast()

const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'hysteria2'] as const
const AUTO_SENTINEL = '__auto__'

const bridges = ref<Bridge[]>([])
const loading = ref(true)

// 抽屉表单状态
const drawerOpen = ref(false)
const editingName = ref<string | null>(null)
const formError = ref('')
const submitting = ref(false)
const form = ref({
  name: '',
  xboard_node_id: 0,
  xboard_node_type: '',
  xui_inbound_id: 0,
  protocol: 'vless',
  flow: '',
  enable: true,
})

// 删除确认 Dialog 状态
const deleteOpen = ref(false)
const deletingBridge = ref<Bridge | null>(null)

const drawerTitle = computed(() =>
  editingName.value ? t('bridges.editTitle', { name: editingName.value }) : t('bridges.addTitle'),
)

// xboard_node_type 业务层留空 ↔ Select 层 AUTO_SENTINEL 双向桥接（同 v0.6）
const selectXboardType = computed({
  get(): string {
    return form.value.xboard_node_type === '' ? AUTO_SENTINEL : form.value.xboard_node_type
  },
  set(v: string) {
    form.value.xboard_node_type = v === AUTO_SENTINEL ? '' : v
  },
})

async function refresh(): Promise<void> {
  loading.value = true
  try {
    bridges.value = await api.listBridges()
  } catch (e) {
    console.warn(e)
    toast({ title: t('errors.loadFailed'), variant: 'destructive' })
  } finally {
    loading.value = false
  }
}

function openCreate(): void {
  editingName.value = null
  form.value = {
    name: '',
    xboard_node_id: 0,
    xboard_node_type: '',
    xui_inbound_id: 0,
    protocol: 'vless',
    flow: '',
    enable: true,
  }
  formError.value = ''
  drawerOpen.value = true
}

function openEdit(b: Bridge): void {
  editingName.value = b.name
  form.value = {
    name: b.name,
    xboard_node_id: b.xboard_node_id,
    xboard_node_type: b.xboard_node_type,
    xui_inbound_id: b.xui_inbound_id,
    protocol: b.protocol,
    flow: b.flow ?? '',
    enable: b.enable,
  }
  formError.value = ''
  drawerOpen.value = true
}

async function submit(): Promise<void> {
  formError.value = ''
  if (!form.value.name) {
    formError.value = t('bridges.errNameEmpty')
    return
  }
  if (form.value.xboard_node_id <= 0 || form.value.xui_inbound_id <= 0) {
    formError.value = t('bridges.errIdsInvalid')
    return
  }
  submitting.value = true
  try {
    if (editingName.value) {
      await api.updateBridge(editingName.value, form.value)
      toast({ title: t('bridges.okUpdated'), variant: 'success' })
    } else {
      await api.createBridge(form.value)
      toast({ title: t('bridges.okCreated'), variant: 'success' })
    }
    drawerOpen.value = false
    await refresh()
  } catch (e) {
    void e
    formError.value = t('errors.requestFailed')
  } finally {
    submitting.value = false
  }
}

function askDelete(b: Bridge): void {
  deletingBridge.value = b
  deleteOpen.value = true
}

async function confirmDelete(): Promise<void> {
  if (!deletingBridge.value) return
  const target = deletingBridge.value
  deleteOpen.value = false
  try {
    await api.deleteBridge(target.name)
    toast({
      title: t('bridges.okDeleted', { name: target.name }),
      variant: 'success',
    })
    await refresh()
  } catch (e) {
    void e
    toast({
      title: t('errors.deleteFailed'),
      variant: 'destructive',
    })
  } finally {
    deletingBridge.value = null
  }
}

// 协议色 helper—— 与 Dashboard 共用 .protocol-chip-* 工具类。
function protocolChipClass(protocol: string): string {
  const p = protocol.toLowerCase()
  if (['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'hysteria2'].includes(p)) {
    return `protocol-chip-${p}`
  }
  return 'protocol-chip-default'
}

onMounted(refresh)
</script>

<template>
  <div class="space-y-5">
    <!-- 页面头 -->
    <header class="flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-foreground">
          {{ t('bridges.title') }}
        </h2>
        <p class="mt-1 text-sm text-muted-foreground">
          {{ t('bridges.subtitle') }}
        </p>
      </div>
      <Button @click="openCreate">
        <Plus aria-hidden="true" />
        {{ t('bridges.addBtn') }}
      </Button>
    </header>

    <!-- 加载占位：3 张 skeleton 卡片 -->
    <section v-if="loading && bridges.length === 0" class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
      <Skeleton v-for="n in 3" :key="n" class="h-32 w-full" />
    </section>

    <!-- 空态：dashed border + CTA -->
    <section
      v-else-if="!loading && bridges.length === 0"
      class="bento-tile"
    >
      <div class="rounded-xl border border-dashed bg-muted/30 px-6 py-12 text-center">
        <AlertCircle class="mx-auto mb-3 h-10 w-10 text-muted-foreground" aria-hidden="true" />
        <p class="text-sm font-medium text-foreground">{{ t('bridges.emptyTitle') }}</p>
        <p class="mt-1 text-xs text-muted-foreground">{{ t('bridges.emptyHint') }}</p>
        <Button class="mt-4" @click="openCreate">
          <Plus aria-hidden="true" />
          {{ t('bridges.addBtn') }}
        </Button>
      </div>
    </section>

    <!-- 卡片网格 -->
    <section v-else class="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
      <article
        v-for="b in bridges"
        :key="b.name"
        class="group relative flex flex-col gap-3 rounded-2xl border bg-card p-5 shadow-bento transition-all duration-200 hover:-translate-y-0.5 hover:border-brand-300 hover:shadow-bento-hover dark:hover:border-brand-700"
      >
        <!-- 顶行：name + 状态 LiveDot + 操作浮按钮 -->
        <div class="flex items-start gap-2">
          <div class="flex-1 min-w-0">
            <p class="truncate text-sm font-semibold text-foreground">{{ b.name }}</p>
            <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
              <span class="protocol-chip" :class="protocolChipClass(b.protocol)">
                {{ b.protocol }}
              </span>
              <span v-if="b.flow" class="font-mono text-[11px] text-muted-foreground">
                {{ b.flow }}
              </span>
            </div>
          </div>
          <LiveDot :status="b.enable ? 'on' : 'off'" size="md" />
        </div>

        <!-- 中行：Xboard / 3x-ui ID 对照 -->
        <div class="grid grid-cols-2 gap-3 text-xs">
          <div>
            <p class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              {{ t('bridges.tableXboardNode') }}
            </p>
            <p class="mt-0.5 font-mono text-foreground">
              #{{ b.xboard_node_id }}
              <span v-if="b.xboard_node_type" class="text-muted-foreground">
                ({{ b.xboard_node_type }})
              </span>
            </p>
          </div>
          <div>
            <p class="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
              {{ t('bridges.tableXuiInbound') }}
            </p>
            <p class="mt-0.5 font-mono text-foreground">#{{ b.xui_inbound_id }}</p>
          </div>
        </div>

        <!-- 底行：状态文字 + 浮按钮（编辑 / 删除）。
             group-hover 时浮按钮 opacity 100；focus-within 让键盘 Tab
             导航时按钮也可见——纯 hover 隐藏会让键盘用户找不到操作。 -->
        <div class="flex items-center justify-between border-t pt-3">
          <span class="text-xs font-medium" :class="b.enable
            ? 'text-brand-700 dark:text-brand-400'
            : 'text-muted-foreground'">
            {{ b.enable ? t('common.enabled') : t('common.disabled') }}
          </span>
          <!--
            按钮揭示策略：触屏（pointer: coarse）常显，鼠标用户 hover/focus 才显示
            （v0.7 第 2 轮 Codex minor 反馈 #7 修复）。.reveal-on-hover 工具类
            （style.css）按 @media (pointer: fine) 区分——避免触屏用户看不到
            编辑/删除入口。
          -->
          <div class="reveal-on-hover flex gap-1">
            <Button
              variant="ghost"
              size="icon"
              class="h-8 w-8"
              :aria-label="t('bridges.actionEdit', { name: b.name })"
              @click="openEdit(b)"
            >
              <Pencil class="size-4" aria-hidden="true" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              class="h-8 w-8 hover:bg-destructive/10 hover:text-destructive"
              :aria-label="t('bridges.actionDelete', { name: b.name })"
              @click="askDelete(b)"
            >
              <Trash2 class="size-4" aria-hidden="true" />
            </Button>
          </div>
        </div>
      </article>
    </section>

    <!--
      抽屉表单（Sheet 右侧滑入）—— v0.6 实现保留：reka-ui 自动处理
      focus trap / Esc 关闭 / 焦点归还。
    -->
    <Sheet v-model:open="drawerOpen">
      <SheetContent side="right" class="flex flex-col">
        <SheetHeader>
          <SheetTitle>{{ drawerTitle }}</SheetTitle>
          <SheetDescription>{{ t('bridges.drawerSubtitle') }}</SheetDescription>
        </SheetHeader>

        <form id="bridge-form" class="flex-1 space-y-5 overflow-y-auto py-6" @submit.prevent="submit">
          <div>
            <Label for="bridge-name">{{ t('bridges.fieldName') }}</Label>
            <Input
              id="bridge-name"
              v-model="form.name"
              :disabled="!!editingName"
              :placeholder="t('bridges.namePlaceholder')"
              class="mt-1.5"
            />
            <p v-if="editingName" class="mt-1.5 text-xs text-muted-foreground">
              {{ t('bridges.nameLockedHint') }}
            </p>
          </div>

          <div class="grid grid-cols-2 gap-4">
            <div>
              <Label for="bridge-xboard-id">{{ t('bridges.fieldXboardId') }}</Label>
              <Input
                id="bridge-xboard-id"
                v-model.number="form.xboard_node_id"
                type="number"
                min="1"
                class="mt-1.5 font-mono"
              />
            </div>
            <div>
              <Label for="bridge-xboard-type">{{ t('bridges.fieldXboardType') }}</Label>
              <Select v-model="selectXboardType">
                <SelectTrigger id="bridge-xboard-type" class="mt-1.5">
                  <SelectValue :placeholder="t('bridges.xboardTypeAuto')" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem :value="AUTO_SENTINEL">{{ t('bridges.xboardTypeAuto') }}</SelectItem>
                  <SelectItem v-for="p in protocols" :key="p" :value="p">{{ p }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div class="grid grid-cols-2 gap-4">
            <div>
              <Label for="bridge-xui-id">{{ t('bridges.fieldXuiId') }}</Label>
              <Input
                id="bridge-xui-id"
                v-model.number="form.xui_inbound_id"
                type="number"
                min="1"
                class="mt-1.5 font-mono"
              />
            </div>
            <div>
              <Label for="bridge-protocol">{{ t('bridges.fieldProtocol') }}</Label>
              <Select v-model="form.protocol">
                <SelectTrigger id="bridge-protocol" class="mt-1.5">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem v-for="p in protocols" :key="p" :value="p">{{ p }}</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>

          <div v-if="form.protocol === 'vless'">
            <Label for="bridge-flow">{{ t('bridges.fieldFlow') }}</Label>
            <Input
              id="bridge-flow"
              v-model="form.flow"
              :placeholder="t('bridges.flowPlaceholder')"
              class="mt-1.5 font-mono"
            />
          </div>

          <div class="flex items-center gap-3">
            <Switch id="bridge-enable" v-model="form.enable" />
            <Label for="bridge-enable" class="cursor-pointer">{{ t('bridges.fieldEnable') }}</Label>
          </div>

          <Alert v-if="formError" variant="destructive" role="alert" aria-live="assertive">
            <AlertCircle />
            <AlertDescription>{{ formError }}</AlertDescription>
          </Alert>
        </form>

        <SheetFooter class="border-t pt-4">
          <Button type="button" variant="outline" @click="drawerOpen = false">
            {{ t('common.cancel') }}
          </Button>
          <Button type="submit" form="bridge-form" :disabled="submitting" :aria-busy="submitting">
            <Loader2 v-if="submitting" class="animate-spin" aria-hidden="true" />
            {{ submitting ? t('common.submitting') : t('common.save') }}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>

    <!-- 删除确认 Dialog—— v0.6 实现保留（reka-ui 自带 focus 管理）。 -->
    <Dialog v-model:open="deleteOpen">
      <DialogContent class="max-w-md">
        <DialogHeader>
          <DialogTitle>{{ t('common.delete') }}</DialogTitle>
          <DialogDescription>
            {{ deletingBridge ? t('bridges.deleteConfirm', { name: deletingBridge.name }) : '' }}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" @click="deleteOpen = false">
            {{ t('common.cancel') }}
          </Button>
          <Button type="button" variant="destructive" @click="confirmDelete">
            {{ t('common.delete') }}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
