// Right-to-left scripts, by ISO 639-1 language subtag. A document's direction
// follows its locale, and `dir` on <html> is what makes a layout's logical
// properties resolve the other way round.
//
// Here rather than in the app that renders: the direction is decided twice per
// load — once by the inline bootstrap snippet a build generates, before any
// bundle parses, and again when the catalogue activates — and the two must not
// be able to disagree.
export const RTL_LANGUAGES: readonly string[] = ['ar', 'fa', 'he', 'ur', 'ps', 'sd', 'ug', 'yi']

export const isRTLLocale = (tag: string): boolean => RTL_LANGUAGES.includes(tag.split('-')[0])
