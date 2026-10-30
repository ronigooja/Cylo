# Screen-document contract (draft)

This is a draft contract for AI agents. The Go contracts in [`/contracts`](../../contracts/) are the implemented authority and do not yet include this model. Before server implementation, mirror and version this contract there.

## Shape

`ScreenDocument` is `{version, revision, root}`. `version` identifies the component-library contract; `revision` is the server ordering token; `root` is a `ScreenNode`.

`ScreenNode` is `{id, type, props, children, emphasis, tone, ttlSeconds, transition, action}`. `children` is an ordered list of nodes. `emphasis` is `low | normal | high`; `tone` is `neutral | info | attention | success | critical`; `ttlSeconds` and `transition` are optional. `action`, when present, is `{intent, args}` and is submitted opaquely to the server. IDs are stable within a document.

## Components

The versioned library contains these generic types. Props are semantic and concise; clients choose layout, colors, typography, and pixels.

- Layout: `stack` (`axis`, `spacing`), `grid` (`columns`), `split` (`orientation`, `ratio`), `group` (`label`), `spacer` (`minLength`).
- Content: `title` (`text`, `level`), `text` (`text`), `richText` (`markdown`), `list` (`items`), `table` (`columns`, `rows`), `timeline` (`events`), `progress` (`value`, `label`), `statusDot` (`state`, `label`), `metric` (`value`, `label`, `delta`), `chart` (`kind`, `series`), `image` (`url`, `alt`), `link` (`label`, `url`), `countdown` (`until`, `label`).
- Interactive: `button` (`label`, `style`), `choice` (`options`, `selected`), `input` (`label`, `value`, `placeholder`, `inputType`). Interactive nodes use `action`; `choice` and `input` include their current value in `args` when submitted.

Unknown component types must degrade to readable text using the node's `props` and child content. A client must not infer a business concept from a type, and the component library is versioned.

## Settings and tools

Setting metadata is `{key, type, range, description, aiMutable, userOnlyReason}`. `userOnlyReason` is present when the user alone may change a setting, including instance key/login credentials, OS microphone permission, and the setting that revokes AI settings permission. AI setting changes show a subtle notice and create a reviewable, undoable audit record.

The macOS client tools are: `screen.show(document)`, `screen.clear()`, `settings.read(key)`, `settings.write(key, value)`, `voice.start()`, `voice.end()`, `textInput.show()`, `textInput.hide()`, and `voice.setSilenceTimeout(seconds)`.

## Authority rule

Tool calls and node actions never decide business outcomes. They submit authenticated opaque intents plus `args`; the server validates authority, scope, expected version, and expiry, then returns authoritative state or a new screen document. Voice never silently submits or confirms business work. With no server document yet, the client may derive a default document from authoritative tasks, approvals, and notifications through the same renderer; it may not fabricate content.
