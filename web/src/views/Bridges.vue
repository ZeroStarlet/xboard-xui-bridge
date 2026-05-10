<script setup lang="ts">
// 桥接管理（v0.6 视觉重构 — shadcn-vue + i18n + 深色 + a11y）。
//
// 视觉策略：
//   - 列表用 shadcn-vue Table 家族
//   - 表单用 Sheet（右侧抽屉）替代 v0.5 的自实现 transition——reka-ui 已处理
//     focus trap / esc 关闭 / 焦点归还，无需手写 watch + nextTick
//   - 删除确认用 Dialog 替代 native confirm()——风格统一、可定制、深色友好
//   - 反馈用 toast 替代 inline alert——非阻塞通知
//   - 表单字段 Input + Label + Select + Switch
//
// i18n：所有文案走 t()，包括 placeholder / aria-label / 错误提示 / toast 文字。
//
// 可访问性：
//   - Sheet 自带 focus trap + esc 关闭 + 焦点归还（reka-ui DialogContent 内建）
//   - Dialog 自带相同 focus 管理
//   - 表格行操作按钮带 aria-label="编辑 {name}" / "删除 {name}"
//   - 表单错误用 Alert role="alert" 朗读
import { ref, onMounted, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Plus, Pencil, Trash2, Loader2, AlertCircle } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import {
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from '@/components/ui/table'
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
import { useToast } from '@/composables/useToast'
import { api, type Bridge } from '@/api/client'

const { t } = useI18n()
const { toast } = useToast()

const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria', 'hysteria2'] as const

// reka-ui 的 SelectItem 禁止 value=""——空字符串被保留作"清空选择"语义。
// 用非空 sentinel "__auto__" 表达"留空 = 按 protocol 推断"，提交时通过
// computed selectXboardType 转换回真实空串发给后端。
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

// xboard_node_type 在 form 内是真实值（''=auto，'vless'/'vmess' 等=指定协议）；
// 在 Select 控件层面用 AUTO_SENTINEL 替代 '' 兼容 reka-ui 限制。两者通过
// computed get/set 透明桥接，业务代码（submit）仍读 form.xboard_node_type
// 的真实值，无需感知 sentinel。
const selectXboardType = computed({
  get(): string {
    return form.value.xboard_node_type === '' ? AUTO_SENTINEL : form.value.xboard_node_type
  },
  set(v: string) {
    form.value.xboard_node_type = v === AUTO_SENTINEL ? '' : v
  },
})

async function refresh() {
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

function openCreate() {
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

function openEdit(b: Bridge) {
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

async function submit() {
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
    // 错误信息走 t() 统一 i18n，与 Login.vue 同策略——损失服务端具体错误码
    // 反馈但保 i18n 一致性。后续可按 e.code 映射到 i18n key 取回细颗粒度。
    void e
    formError.value = t('errors.requestFailed')
  } finally {
    submitting.value = false
  }
}

function askDelete(b: Bridge) {
  deletingBridge.value = b
  deleteOpen.value = true
}

async function confirmDelete() {
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
    // i18n 一致：错误信息走 t() 不直显服务端原始消息（详见 submit 注释）。
    void e
    toast({
      title: t('errors.deleteFailed'),
      variant: 'destructive',
    })
  } finally {
    deletingBridge.value = null
  }
}

onMounted(refresh)
</script>

<template>
  <div>
    <!-- 页面头 -->
    <header class="mb-7 flex items-center justify-between">
      <div>
        <h2 class="text-2xl font-semibold tracking-tight text-foreground">{{ t('bridges.title') }}</h2>
        <p class="mt-1 text-sm text-muted-foreground">{{ t('bridges.subtitle') }}</p>
      </div>
      <Button @click="openCreate">
        <Plus aria-hidden="true" />
        {{ t('bridges.addBtn') }}
      </Button>
    </header>

    <!-- 主表格——Card 默认已有 border/bg-card/text-card-foreground 类，无需重复 -->
    <Card>
      <CardContent class="p-6">
        <div
          v-if="!loading && bridges.length === 0"
          class="rounded-xl border border-dashed bg-muted/30 px-6 py-12 text-center"
        >
          <AlertCircle class="mx-auto mb-3 h-10 w-10 text-muted-foreground" aria-hidden="true" />
          <p class="text-sm font-medium text-foreground">{{ t('bridges.emptyTitle') }}</p>
          <p class="mt-1 text-xs text-muted-foreground">{{ t('bridges.emptyHint') }}</p>
        </div>

        <Table v-else>
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('bridges.tableName') }}</TableHead>
              <TableHead>{{ t('bridges.tableProtocol') }}</TableHead>
              <TableHead>{{ t('bridges.tableXboardNode') }}</TableHead>
              <TableHead>{{ t('bridges.tableXuiInbound') }}</TableHead>
              <TableHead>{{ t('bridges.tableState') }}</TableHead>
              <TableHead class="text-right">{{ t('bridges.tableActions') }}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow v-for="b in bridges" :key="b.name">
              <TableCell class="font-medium text-foreground">{{ b.name }}</TableCell>
              <TableCell>
                <span class="font-mono text-[13px]">{{ b.protocol }}</span>
                <span v-if="b.flow" class="ml-1 text-xs text-muted-foreground">({{ b.flow }})</span>
              </TableCell>
              <TableCell>
                <span class="font-mono text-[13px]">{{ b.xboard_node_id }}</span>
                <span class="ml-1 text-xs text-muted-foreground">({{ b.xboard_node_type }})</span>
              </TableCell>
              <TableCell><span class="font-mono text-[13px]">{{ b.xui_inbound_id }}</span></TableCell>
              <TableCell>
                <Badge :variant="b.enable ? 'success' : 'secondary'">
                  <span class="size-1.5 rounded-full bg-current" aria-hidden="true" />
                  {{ b.enable ? t('common.enabled') : t('common.disabled') }}
                </Badge>
              </TableCell>
              <TableCell>
                <div class="flex justify-end gap-1">
                  <Button
                    variant="ghost"
                    size="icon"
                    :aria-label="t('bridges.actionEdit', { name: b.name })"
                    @click="openEdit(b)"
                  >
                    <Pencil aria-hidden="true" />
                  </Button>
                  <Button
                    variant="ghost"
                    size="icon"
                    class="hover:bg-destructive/10 hover:text-destructive"
                    :aria-label="t('bridges.actionDelete', { name: b.name })"
                    @click="askDelete(b)"
                  >
                    <Trash2 aria-hidden="true" />
                  </Button>
                </div>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>
      </CardContent>
    </Card>

    <!--
      抽屉表单（Sheet 右侧滑入）
      reka-ui Dialog primitives 已自动处理：
        - focus trap（焦点不能 Tab 到 sheet 外）
        - Esc 关闭
        - 关闭时焦点归还到触发器（v0.5 需要手写 lastTrigger ref + watch + nextTick）
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
              <!--
                v-model 绑到 selectXboardType（computed bridge）而非 form.xboard_node_type
                直接：reka-ui 禁止 SelectItem value=""，所以 sentinel '__auto__'
                作为"留空"的占位值，computed 在表层与业务层之间双向转换。
              -->
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

    <!--
      删除确认 Dialog——替代 v0.5 的 native confirm()：
      跨浏览器视觉一致、深色模式适配、可加图标 / 可定制按钮文字。
    -->
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
