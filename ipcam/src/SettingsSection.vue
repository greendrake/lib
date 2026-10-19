<script lang="ts">
import type { SelectBoxOption } from '@greendrake/ui'
import type { Option } from './types'

// A choice's options as the select box takes them.
const choices = (options: Option[]): SelectBoxOption<string>[] => options.map(({ Value: value, Label: label }) => ({ value, label }))
</script>
<script setup lang="ts">
import { ref, shallowRef } from 'vue'
import { Checkbox, Field, InfoTip, SelectBox, TextField, useAsyncAction } from '@greendrake/ui'
import { useCameraApi } from './api'
import { checkText, draftsOf, isText, settle, writeOf, type Drafts, type Writable } from './fields'
import Problem from './Problem.vue'
import { wording } from './problems'
import type { Group, Setting } from './types'

const props = defineProps<{ group: Group }>()

const api = useCameraApi()

// The group's settings as the camera has them: read once the section first shows, which
// keeps them while another shows, and then as each write and reload answers. Replaced,
// never edited, so not deeply reactive.
const settings = shallowRef<Setting[]>([])
const drafts = ref(draftsOf([]))

// A change is written at once. Writes and reads go out one at a time, in the order they
// were asked for, so that none overtakes another.
let queue = Promise.resolve()
const writes = ref(0)
const saved = ref(false)
const problem = ref<string | null>(null)

const enqueue = (op: () => Promise<void>): Promise<void> =>
    (queue = queue.then(op).catch(e => {
        problem.value = wording(e)
    }))

// Writes a setting, and takes what the camera then has of every setting the write can
// have changed: it can take a value and keep another.
const write = async (setting: Writable): Promise<void> => {
    const value = writeOf(setting, drafts.value)
    // A text that is not yet a value says so itself, and is written once it is one.
    if (value === undefined) {
        return
    }
    const sent: Drafts = {
        text: { ...drafts.value.text },
        switch: { ...drafts.value.switch },
        choice: { ...drafts.value.choice }
    }
    // A write the camera takes only once restarted is reported by the banner.
    // "Saving…" says it is out.
    const { Changed } = await api.call('settings.set', { Key: setting.Key, Value: value })
    settings.value = settings.value.map(s => Changed.find(c => c.Key === s.Key) ?? s)
    settle(drafts.value, sent, draftsOf(Changed))
    saved.value = true
}

const apply = (setting: Writable): void => {
    writes.value++
    problem.value = null
    void enqueue(() => write(setting)).finally(() => {
        writes.value--
    })
}

// While the group is read, the section shows nothing but the spinner.
const read = useAsyncAction(() =>
    enqueue(async () => {
        settings.value = await api.call('settings.get', { Group: props.group.ID })
        drafts.value = draftsOf(settings.value)
    })
)
void read.run()
</script>
<template>
    <section :id="group.ID" class="SettingsSection" :class="{ spinner: read.acting.value }">
        <template v-if="!read.acting.value">
            <div class="SettingsSection__fields">
                <div v-for="setting in settings" :key="setting.Key" class="SettingsSection__field">
                    <!-- A text is written once it is committed: on leaving the field, or Enter. -->
                    <Field v-if="isText(setting)" :label="setting.Label" @change="apply(setting)">
                        <TextField v-model="drafts.text[setting.Key]" :validator="(text: string) => checkText(setting, text)" />
                    </Field>
                    <Checkbox v-else-if="setting.Type === 'switch'" v-model="drafts.switch[setting.Key]" :label="setting.Label" switch @update:model-value="apply(setting)" />
                    <Field v-else-if="setting.Type === 'choice'" :label="setting.Label" unlabelable>
                        <SelectBox v-model="drafts.choice[setting.Key]" :options="choices(setting.Options)" :ariaLabel="setting.Label" @update:model-value="apply(setting)" />
                    </Field>
                    <Field v-else-if="setting.Type === 'pinned'" :label="setting.Label" unlabelable>
                        <output>{{ setting.Value }}</output>
                        <button v-if="setting.Value !== setting.Only" type="button" @click="apply(setting)">Switch to {{ setting.Only }}</button>
                    </Field>
                    <Field v-else :label="setting.Label" unlabelable>
                        <output>{{ setting.Value }}</output>
                    </Field>
                    <InfoTip v-if="setting.Tip" class="SettingsSection__tip" :text="setting.Tip" />
                </div>
            </div>
            <slot />
            <div class="SettingsSection__actions">
                <button type="button" @click="read.run()">Reload</button>
                <span v-if="writes > 0">Saving…</span>
                <Problem v-else-if="problem" :text="problem" />
                <span v-else-if="saved">Saved</span>
            </div>
        </template>
    </section>
</template>
<style lang="scss">
// A tab's panel, which scrolls under its tab strip.
.SettingsSection {
    overflow: auto;
    padding: var(--space-3) var(--space-4) var(--space-4);
    &__fields {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
        gap: var(--space-3);
        align-items: end;
    }
    // The tip sits in a gutter at the field's top end, level with its caption and out
    // of its label; every field keeps the gutter, so that the fields line up.
    &__field {
        display: grid;
        grid-template-columns: minmax(0, 1fr) 18px;
        column-gap: var(--space-1);
        align-items: start;
    }
    &__tip {
        grid-column: 2;
        grid-row: 1;
    }
    &__actions {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        margin-top: var(--space-3);
    }
}
</style>
