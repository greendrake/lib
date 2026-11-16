# @greendrake/rpckit

Connects a data table to a server's paged list methods: it turns the table's search text, sort and range of rows into the arguments that every list method of the server takes. It is the client side of the paged-list convention of the Go `greendrake.nz/go/rpckit`.

```sh
bun add @greendrake/rpckit vue
```

`vue` is a peer dependency, and `@greendrake/ui-data` installs as a dependency. The package ships its TypeScript source, with no compiled output and no `.d.ts`, so the consuming app's bundler has to compile TypeScript, as Vite does.

## Why

Every list method of a server that follows the convention takes the same arguments — `q`, `offset`, `limit`, `sort_by`, `sort_dir` — and answers `{ data, total }`. An `@greendrake/ui-data` table loads its rows through a `LoaderFunction`, which is called with the table's own values: the search text, the sort, a range of rows.

Without this package, every table of a dashboard translates one into the other by hand, and every table with a filter wires its own re-fetch for when the filter changes. This package writes both once, in `listLoader` and `narrowedList`.

## Trade-offs

- **Tied to the convention.** A server whose lists take other arguments, or answer another shape, gets nothing from this package. The convention pages by offset and limit, and there is no cursor.
- **Tied to `@greendrake/ui-data` and Vue.** It produces that package's `LoaderFunction`, and `narrowedList` returns a Vue `computed`.

## Surface

The package makes no calls itself. You pass it the function that calls your server, so it works over any transport.

- `PageArgs` is `{ q?, offset?, limit?, sort_by?, sort_dir? }`, the arguments every paged list method parses. A method that honours only some of them ignores the rest.
- `listLoader<T>(fetch)` makes an `@greendrake/ui-data` `LoaderFunction<T>` from a list call, `(args: PageArgs, background: boolean) => Promise<LoaderResponse<T>>`. The table's search text goes as `q`. Its order, when it has one, goes as `sort_by` and `sort_dir`. A method's own filters are added inside `fetch`. The `background` flag is passed on unchanged — here to `listOrders`, the app's own call of its `admin.order.list` method:

```ts
const loader = listLoader((args, background) => listOrders({ ...args, status }, background))
```

The loader does not touch the table's `loading` ref. The table shows its spinner itself while the viewport overlaps rows not yet loaded.

- `narrowedList<N, T>(narrowing, fetch)` is for a list filtered by arguments of the method's own. It returns `{ filters, loader }`, as a table takes them. `filters` is a Vue `computed` of `narrowing()`: the current arguments, with `undefined` for one not given. Pass it as the table's `filters`, and a change to any of them re-fetches. `loader` is a `listLoader`. For each page it calls `fetch` with the page arguments and the value of `narrowing()` at that moment:

```ts
const status = ref<'' | 'open' | 'closed'>('')
const { filters, loader } = narrowedList(
    () => ({ status: status.value || undefined }),
    (args, background) => listOrders(args, background)
)
```
