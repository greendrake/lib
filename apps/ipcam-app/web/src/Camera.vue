<script setup lang="ts">
// The camera's tile, the whole page, once the cameras push has named the camera.
import { CameraTile, provideCameraApi } from '@greendrake/ipcam'
import { api, camera, cameraName, streamHubURL } from '@/api'

provideCameraApi(api)
</script>
<template>
    <CameraTile v-if="cameraName" class="Camera" :hub-url="streamHubURL" :name="cameraName" :state="camera" contain />
</template>
<style lang="scss">
// The body is the theme's flex row, stretching the tile to its height; across, the tile,
// whose content is all positioned, has no width of its own to take.
.Camera {
    flex: 1;
}
</style>
