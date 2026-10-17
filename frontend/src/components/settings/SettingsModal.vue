<script setup lang="ts">
import { onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import SettingsPanel from '@/components/settings/SettingsPanel.vue'
import { useSettingsStore } from '@/stores/settings'

const settings = useSettingsStore()
const route = useRoute()
const router = useRouter()

function close() {
  settings.closeSettings()
  if (route.name === 'settings') void router.push({ name: 'clusters' })
}

watch(
  () => route.name,
  (name) => {
    if (name === 'settings') settings.openSettings()
  },
)

onMounted(() => {
  if (route.name === 'settings') settings.openSettings()
})
</script>

<template>
  <div
    v-if="settings.settingsOpen"
    class="fixed inset-0 z-[60] flex items-center justify-center bg-black/55 p-4 pt-10"
    @click.self="close"
  >
    <div
      class="flex h-[min(820px,92vh)] w-full max-w-5xl flex-col overflow-hidden rounded-2xl border border-line bg-surface-1 shadow-2xl"
      role="dialog"
      aria-label="Settings"
    >
      <SettingsPanel embedded @close="close" />
    </div>
  </div>
</template>
