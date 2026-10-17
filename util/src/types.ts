// The union a host's declaration map discriminates, one member per key: the
// key under `Tag`, and the key's type under `Value` — absent for a key declared
// `never`. Undeclared (an empty map), any string tags it and its value is
// unknown. The map is an interface a package exports empty for the host to
// augment, so the package's own code compiles against the undeclared form.
export type Declared<Map, Tag extends PropertyKey, Value extends PropertyKey> = [keyof Map] extends [never] ? { [P in Tag]: string } & { [P in Value]?: unknown } : { [K in keyof Map]: { [P in Tag]: K } & ([Map[K]] extends [never] ? unknown : { [P in Value]: Map[K] }) }[keyof Map]
