<script setup lang="ts">
// DialogTrigger——透传到 reka-ui DialogTrigger。
import { DialogTrigger, type DialogTriggerProps } from 'reka-ui'

defineProps<DialogTriggerProps>()
</script>

<template>
  <DialogTrigger v-bind="$props">
    <slot />
  </DialogTrigger>
</template>
