<script setup lang="ts">
import { isEmailValid } from '@greendrake/domains'
import TextField from './TextField.vue'
import { messages } from './messages'

const model = defineModel<string>({ default: '' })
defineProps<{
    placeholder?: string
    autofocus?: boolean
    id?: string
    validateOnBlur?: boolean
}>()
const validator = (v: string): true | string => isEmailValid(v) || messages.invalidEmail
</script>
<template>
    <TextField :id="id" v-model="model" type="email" :validator="validator" :placeholder="placeholder" :autofocus="autofocus" :validate-on-blur="validateOnBlur" v-bind="$attrs" />
</template>
