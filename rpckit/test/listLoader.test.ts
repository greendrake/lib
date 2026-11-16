import { describe, expect, test } from 'bun:test'
import { ref } from 'vue'
import type { LoaderParams } from '@greendrake/ui-data'
import { listLoader, narrowedList, type PageArgs } from '../src/main'

// What the list call is asked for when the table loads `params`, and whether
// in the background.
const load = async (params: LoaderParams, background = false): Promise<[PageArgs, boolean]> => {
    let asked: [PageArgs, boolean] | undefined
    const loader = listLoader<never>((args, bg) => {
        asked = [args, bg]
        return Promise.resolve({ data: [], total: 0 })
    })
    await loader(params, ref(false), background)
    return asked!
}

// The first window of a table with nothing else to say.
const FIRST: LoaderParams = {
    offset: 0,
    limit: 50,
    filters: {}
}

describe('listLoader', () => {
    test('the bare window, for a table with no text and no order', async () => {
        expect(await load(FIRST)).toEqual([{ offset: 0, limit: 50 }, false])
    })

    test('the text as q and the order as sort_by / sort_dir', async () => {
        const [args] = await load({
            ...FIRST,
            text: 'ann',
            sort: { by: 'email', dir: 'asc' }
        })
        expect(args).toEqual({
            offset: 0,
            limit: 50,
            q: 'ann',
            sort_by: 'email',
            sort_dir: 'asc'
        })
    })

    test("the table's filters stay out: a method's own ride the fetch closure", async () => {
        const [args] = await load({
            ...FIRST,
            filters: { status: 'open' }
        })
        expect(args).toEqual({ offset: 0, limit: 50 })
    })

    test('a background load says so', async () => {
        expect((await load(FIRST, true))[1]).toBe(true)
    })
})

describe('narrowedList', () => {
    // A list narrowed by a status, and what its call was asked last.
    const narrowed = () => {
        const status = ref('')
        const asked: [PageArgs & { status: string | undefined }, boolean][] = []
        const list = narrowedList(
            () => ({ status: status.value || undefined }),
            (args, background) => {
                asked.push([args, background])
                return Promise.resolve({ data: [], total: 0 })
            }
        )
        return {
            status,
            asked,
            ...list
        }
    }

    test('the filters are the narrowing as it stands', () => {
        const { status, filters } = narrowed()
        expect(filters.value).toEqual({ status: undefined })
        status.value = 'open'
        expect(filters.value).toEqual({ status: 'open' })
    })

    test('the call is asked the page and the narrowing as it stands then', async () => {
        const { status, asked, loader } = narrowed()
        status.value = 'open'
        await loader({ ...FIRST, text: 'ann' }, ref(false), true)
        expect(asked).toEqual([
            [
                {
                    offset: 0,
                    limit: 50,
                    q: 'ann',
                    status: 'open'
                },
                true
            ]
        ])
    })
})
