<script setup lang="ts">
import { onMounted } from 'vue'

import SettingsPanel from '@/components/settings/SettingsPanel.vue'
import { useSettingsStore } from '@/stores/settings'

const settings = useSettingsStore()

onMounted(() => settings.openSettings('general'))
</script>

<template>
  <div class="h-full overflow-hidden bg-surface-0 px-4 py-4">
    <div class="mx-auto flex h-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-line bg-surface-1">
      <SettingsPanel @close="settings.closeSettings()" />
    </div>
  </div>
</template>
