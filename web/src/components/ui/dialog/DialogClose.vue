<script setup lang="ts">
// DialogClose——透传到 reka-ui DialogClose。
import { DialogClose, type DialogCloseProps } from 'reka-ui'

defineProps<DialogCloseProps>()
</script>

<template>
  <DialogClose v-bind="$props">
    <slot />
  </DialogClose>
</template>
