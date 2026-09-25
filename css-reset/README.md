# @greendrake/css-reset

Modern CSS reset for SPA/PWA projects.

Adapted from [Josh Comeau's custom CSS reset](https://www.joshwcomeau.com/css/custom-css-reset/) and [Andy Bell's more modern CSS reset](https://piccalil.li/blog/a-more-modern-css-reset/), with form-control resets added.

## Usage

```scss
@use '@greendrake/css-reset';
```

Note: the reset is aggressive about form controls (`input`, `button`, `textarea`, `select` inherit fonts and lose native styling). It is intended for design systems that fully restyle controls.
