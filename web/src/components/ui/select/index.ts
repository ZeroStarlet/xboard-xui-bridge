// Select 模块 barrel——下拉选择器组件家族。
//
// 用法（精简版）：
//
//   <Select v-model="form.protocol">
//     <SelectTrigger>
//       <SelectValue placeholder="选择协议" />
//     </SelectTrigger>
//     <SelectContent>
//       <SelectItem value="vless">VLESS</SelectItem>
//       <SelectItem value="vmess">VMess</SelectItem>
//       <SelectItem value="trojan">Trojan</SelectItem>
//     </SelectContent>
//   </Select>
//
// 分组用法：
//
//   <SelectContent>
//     <SelectGroup>
//       <SelectLabel>主流协议</SelectLabel>
//       <SelectItem value="vless">VLESS</SelectItem>
//       <SelectItem value="vmess">VMess</SelectItem>
//     </SelectGroup>
//     <SelectSeparator />
//     <SelectGroup>
//       <SelectLabel>高速协议</SelectLabel>
//       <SelectItem value="hysteria2">Hysteria2</SelectItem>
//     </SelectGroup>
//   </SelectContent>
export { default as Select } from './Select.vue'
export { default as SelectTrigger } from './SelectTrigger.vue'
export { default as SelectValue } from './SelectValue.vue'
export { default as SelectContent } from './SelectContent.vue'
export { default as SelectItem } from './SelectItem.vue'
export { default as SelectGroup } from './SelectGroup.vue'
export { default as SelectLabel } from './SelectLabel.vue'
export { default as SelectSeparator } from './SelectSeparator.vue'
export { default as SelectScrollUpButton } from './SelectScrollUpButton.vue'
export { default as SelectScrollDownButton } from './SelectScrollDownButton.vue'
